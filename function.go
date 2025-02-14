package function

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

type Notification struct {
	Incident Incident `json:"incident"`
	Version  string   `json:"version"`
}

type Incident struct {
	ProjectID     string `json:"scoping_project_id"`
	IncidentID    string `json:"incident_id"`
	ResourceID    string `json:"resource_id"`
	ResourceName  string `json:"resource_name"`
	State         string `json:"state"`
	StartedAt     int64  `json:"started_at"`
	EndedAt       int64  `json:"ended_at,omitempty"`
	PolicyName    string `json:"policy_name"`
	ConditionName string `json:"condition_name"`
	URL           string `json:"url"`
	Summary       string `json:"summary"`
}

type DiscordWebhook struct {
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Content   string         `json:"content,omitempty"`
	Embeds    []DiscordEmbed `json:"embeds,omitempty"`
}

type DiscordEmbed struct {
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	URL         string                 `json:"url,omitempty"`
	Color       int                    `json:"color,omitempty"`
	Fields      []DiscordEmbedField    `json:"fields,omitempty"`
	Footer      *DiscordEmbedFooter    `json:"footer,omitempty"`
	Timestamp   string                 `json:"timestamp,omitempty"`
}

type DiscordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type DiscordEmbedFooter struct {
	Text string `json:"text"`
}

func toDiscord(notification Notification) DiscordWebhook {
	var startedDt time.Time
	var endedDt time.Time

	if st := notification.Incident.StartedAt; st > 0 {
		startedDt = time.Unix(st, 0)
	}

	if et := notification.Incident.EndedAt; et > 0 {
		endedDt = time.Unix(et, 0)
	}

	projectId := notification.Incident.ProjectID
	if projectId == "" {
		projectId = "-"
		log.Printf("Warning: Empty ProjectID received")
	}

	policyName := notification.Incident.PolicyName
	if policyName == "" {
		policyName = "-"
		log.Printf("Warning: Empty PolicyName received")
	}

	conditionName := notification.Incident.ConditionName
	if conditionName == "" {
		conditionName = "-"
		log.Printf("Warning: Empty ConditionName received")
	}

	fields := []DiscordEmbedField{
		{
			Name:   "Project ID",
			Value:  projectId,
			Inline: true,
		},
		{
			Name:   "Incident ID",
			Value:  notification.Incident.IncidentID,
			Inline: true,
		},
		{
			Name:   "Condition",
			Value:  conditionName,
			Inline: true,
		},
	}

	if !startedDt.IsZero() {
		fields = append(fields, DiscordEmbedField{
			Name:   "Started at",
			Value:  startedDt.Format(time.RFC3339),
			Inline: true,
		})
		if !endedDt.IsZero() {
			duration := strings.TrimSpace(humanize.RelTime(startedDt, endedDt, "", ""))
			fields = append(fields, DiscordEmbedField{
				Name:   "Ended at",
				Value:  fmt.Sprintf("%s (%s)", endedDt.Format(time.RFC3339), duration),
				Inline: true,
			})
		}
	}

	// Green
	color := 1619771 // #18FF3B in decimal
	title := fmt.Sprintf(`"%s" - Incident closed for "%s"`, projectId, policyName)
	if notification.Incident.State == "open" {
		// Red
		color = 16007725 // #F5222D in decimal
		title = fmt.Sprintf(`"%s" - Incident opened for "%s"`, projectId, policyName)
	}

	summary := "No summary available."
	if notification.Incident.Summary != "" {
		summary = notification.Incident.Summary
	} else {
		log.Printf("Warning: Empty Summary received")
	}

	return DiscordWebhook{
		Username:  "GCP Monitoring",
		AvatarURL: "https://www.gstatic.com/images/branding/product/2x/stackdriver_64dp.png",
		Embeds: []DiscordEmbed{
			{
				Title:       title,
				Description: summary,
				URL:         notification.Incident.URL,
				Color:       color,
				Fields:      fields,
				Timestamp:   time.Now().Format(time.RFC3339),
				Footer: &DiscordEmbedFooter{
					Text: "GCP Monitoring Alert",
				},
			},
		},
	}
}

func F(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request: Method=%s, ContentType=%s", r.Method, r.Header.Get("Content-Type"))

	authToken := os.Getenv("AUTH_TOKEN")
	if authToken == "" {
		log.Fatalln("`AUTH_TOKEN` is not set in the environment")
	}

	receivedToken := r.URL.Query().Get("auth_token")
	if receivedToken != authToken {
		log.Printf("Auth token mismatch. Received: '%s'", receivedToken)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid request"))
		return
	}

	discordWebhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if discordWebhookURL == "" {
		log.Fatalln("`DISCORD_WEBHOOK_URL` is not set in the environment")
	}

	if _, err := url.Parse(discordWebhookURL); err != nil {
		log.Fatalln(err)
	}

	if contentType := r.Header.Get("Content-Type"); r.Method != "POST" || contentType != "application/json" {
		log.Printf("Invalid method / content-type: %s / %s", r.Method, contentType)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid request"))
		return
	}

	var notification Notification
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		log.Printf("Error decoding notification: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid request body"))
		return
	}

	// Log the received notification
	// notificationJSON, _ := json.MarshalIndent(notification, "", "  ")
	// log.Printf("Received notification:\n%s", string(notificationJSON))

	discordWebhook := toDiscord(notification)

	payload, err := json.Marshal(discordWebhook)
	if err != nil {
		log.Printf("Error marshaling discord webhook: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Log the Discord webhook payload
	// log.Printf("Sending Discord webhook:\n%s", string(payload))

	res, err := http.Post(discordWebhookURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("Error posting to discord: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("Unexpected Discord response status: %d", res.StatusCode)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// log.Printf("Successfully sent to Discord with status: %d", res.StatusCode)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(discordWebhook); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}
