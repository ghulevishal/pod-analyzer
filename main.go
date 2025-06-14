package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	OLLAMA_API     = "http://192.168.0.113:11434/api/generate"
	OLLAMA_MODEL   = "llama3"
	SLACK_CHANNEL  = "#all-vishal-personal"
	CHECK_INTERVAL = 30 * time.Second
	LOG_LINES      = 50
)

var notifiedRestarts = make(map[string]time.Time)

func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Println("⚠️ In-cluster config not found, trying local kubeconfig...")
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Fatalf("❌ Failed to load kubeconfig: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("❌ Failed to create clientset: %v", err)
	}

	log.Println("🚀 Pod restart monitor started...")

	for {
		pods, err := clientset.CoreV1().Pods("").List(context.Background(), v1.ListOptions{})
		if err != nil {
			log.Printf("❌ Error fetching pods: %v", err)
			continue
		}

		for _, pod := range pods.Items {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.RestartCount > 0 && pod.Status.StartTime != nil {
					key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
					restartTime := pod.Status.StartTime.Time

					if last, exists := notifiedRestarts[key]; !exists || restartTime.After(last) {
						notifiedRestarts[key] = restartTime
						log.Printf("🚨 Detected restart: %s [%s]", pod.Name, pod.Namespace)
						go analyzePod(clientset, pod.Name, pod.Namespace, restartTime)
					}
				}
			}
		}
		time.Sleep(CHECK_INTERVAL)
	}
}

func analyzePod(clientset *kubernetes.Clientset, podName, namespace string, restartTime time.Time) {
	ctx := context.Background()

	logs, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{TailLines: int64Ptr(LOG_LINES)}).DoRaw(ctx)
	if err != nil {
		log.Printf("❌ Failed to get logs for %s: %v", podName, err)
		return
	}

	eventList, err := clientset.CoreV1().Events(namespace).List(ctx, v1.ListOptions{})
	if err != nil {
		log.Printf("❌ Failed to get events for %s: %v", podName, err)
		return
	}

	var events []corev1.Event
	for _, e := range eventList.Items {
		if e.InvolvedObject.Name == podName && e.LastTimestamp.Time.After(restartTime.Add(-1*time.Minute)) {
			events = append(events, e)
		}
	}

	analysis, err := callOllama(logs, events)
	if err != nil {
		log.Printf("❌ Failed to analyze pod %s: %v", podName, err)
		return
	}

	threadTS := sendMainSlackMessage(podName, namespace, restartTime)
	if threadTS != "" {
		sendSlackThread(threadTS, "📋 *Events:*\n```"+formatEvents(events)+"```")
		sendSlackThread(threadTS, "📦 *Logs:*\n```"+truncate(string(logs), 1000)+"```")
		sendSlackThread(threadTS, "🤖 *Analysis:*\n"+formatCodeBlocks(truncate(analysis, 3000)))
	}
}

func callOllama(logs []byte, events []corev1.Event) (string, error) {
	eventLines := []string{}
	for _, e := range events {
		eventLines = append(eventLines, fmt.Sprintf("- %s: %s", e.Reason, e.Message))
	}
	eventStr := strings.Join(eventLines, "\n")

	prompt := fmt.Sprintf("Here are the logs and events from a Kubernetes pod. Help me identify the issue and suggest a fix.\n\nEvents:\n%s\n\nLogs:\n%s", eventStr, string(logs))
	body := map[string]interface{}{
		"model":  OLLAMA_MODEL,
		"prompt": prompt,
		"stream": false,
	}
	jsonData, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", OLLAMA_API, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}

	if response, ok := parsed["response"].(string); ok {
		return response, nil
	}
	return "No response from model", nil
}

func sendMainSlackMessage(podName, namespace string, restartTime time.Time) string {
	summary := "*🚨 Pod Restart Detected!*\n" +
		fmt.Sprintf("> *Pod:* `%s`\n", podName) +
		fmt.Sprintf("> *Namespace:* `%s`\n", namespace) +
		fmt.Sprintf("> *Restart Time:* `%s`", restartTime.Format("2006-01-02 15:04:05"))

	payload := map[string]interface{}{
		"channel": SLACK_CHANNEL,
		"text":    summary,
	}
	return postToSlack(payload)
}

func sendSlackThread(threadTs string, message string) {
	payload := map[string]interface{}{
		"channel":   SLACK_CHANNEL,
		"text":      message,
		"thread_ts": threadTs,
	}
	postToSlack(payload)
}

func postToSlack(payload map[string]interface{}) string {
	token := os.Getenv("SLACK_BOT_TOKEN")
	url := "https://slack.com/api/chat.postMessage"

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("❌ Slack API error: %v", err)
		return ""
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(body, &result)

	if ok, _ := result["ok"].(bool); !ok {
		log.Printf("❌ Slack API response: %s", string(body))
		return ""
	}

	if ts, ok := result["ts"].(string); ok {
		return ts
	}
	return ""
}

func formatEvents(events []corev1.Event) string {
	var lines []string
	for _, e := range events {
		lines = append(lines, fmt.Sprintf("%s: %s", e.Reason, e.Message))
	}
	return strings.Join(lines, "\n")
}

func formatCodeBlocks(text string) string {
	lines := strings.Split(text, "\n")
	var formatted []string
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "kubectl ") || strings.HasPrefix(trimmed, "bash ") {
			if !inCodeBlock {
				formatted = append(formatted, "```bash")
				inCodeBlock = true
			}
			formatted = append(formatted, trimmed)
			continue
		}
		if inCodeBlock {
			formatted = append(formatted, "```")
			inCodeBlock = false
		}
		formatted = append(formatted, trimmed)
	}
	if inCodeBlock {
		formatted = append(formatted, "```")
	}
	return strings.Join(formatted, "\n")
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "... (truncated)"
}

func int64Ptr(i int64) *int64 {
	return &i
}