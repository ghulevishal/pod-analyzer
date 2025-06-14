# pod-analyzer
# pod-analyzer



✅ Step 1: Create Slack App
	1.	Go to https://api.slack.com/apps
	2.	Click “Create New App”
	3.	Choose “From scratch”
	•	Name it something like PodWatcher
	•	Choose your Slack workspace

⸻

✅ Step 2: Add Bot Permissions
	1.	In the left sidebar: OAuth & Permissions
	2.	Under Scopes, add:
	•	chat:write
	•	chat:write.public
	3.	Under Install App, click “Install to Workspace”
	4.	Copy the Bot User OAuth Token — starts with xoxb-...

 
### export the SLACK_BOT_TOKEN

```
export SLACK_BOT_TOKEN
```

1. Go to your Slack app dashboard

👉 https://api.slack.com/apps → Your App → OAuth & Permissions

2. Add these OAuth scopes under “Bot Token Scopes”:
	•	chat:write ✅
	•	(Optional but useful):
	•	channels:read
	•	groups:read

❗ You may see chat:write.customize or chat:write.public — those are different. You need plain chat:write.

3. Reinstall your app

Scroll up in the OAuth page and hit the “Reinstall to Workspace” button. Slack will reauthorize the app with updated scopes.
