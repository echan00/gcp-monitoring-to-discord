package function

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

// GCP Notification structures
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

// Adapty Event structures
type AdaptyEvent struct {
	Event     string         `json:"event"`
	EventType string         `json:"event_type"`
	Timestamp int64          `json:"timestamp"`
	Data      AdaptyEventData `json:"data"`
}

type AdaptyEventData struct {
	ProfileID       string              `json:"profile_id"`
	CustomerUserID  string              `json:"customer_user_id,omitempty"`
	Subscription    *AdaptySubscription `json:"subscription,omitempty"`
	Transaction     *AdaptyTransaction  `json:"transaction,omitempty"`
	NewStatus       string              `json:"new_status,omitempty"`
	PreviousStatus  string              `json:"previous_status,omitempty"`
	Product         *AdaptyProduct      `json:"product,omitempty"`
}

type AdaptySubscription struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Store           string `json:"store"`
	ProductID       string `json:"product_id"`
	ExpiresAt       int64  `json:"expires_at,omitempty"`
	CanceledAt      int64  `json:"canceled_at,omitempty"`
	StartedAt       int64  `json:"started_at,omitempty"`
	RenewedAt       int64  `json:"renewed_at,omitempty"`
	IsSandbox       bool   `json:"is_sandbox"`
}

type AdaptyTransaction struct {
	ID              string  `json:"id"`
	OfferID         string  `json:"offer_id,omitempty"`
	ProductID       string  `json:"product_id"`
	PurchasedAt     int64   `json:"purchased_at"`
	IsRestored      bool    `json:"is_restored"`
	Value           float64 `json:"value,omitempty"`
	Currency        string  `json:"currency,omitempty"`
	Store           string  `json:"store"`
	IsSandbox       bool    `json:"is_sandbox"`
}

type AdaptyProduct struct {
	VendorProductID string `json:"vendor_product_id"`
	BasePlanID      string `json:"base_plan_id,omitempty"`
}

// Discord webhook structures
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

// Convert GCP notification to Discord webhook
func gcpToDiscord(notification Notification) DiscordWebhook {
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

	// Green for closed incidents
	color := 1619771 // #18FF3B in decimal
	title := fmt.Sprintf(`"%s" - Incident closed for "%s"`, projectId, policyName)
	if notification.Incident.State == "open" {
		// Red for open incidents
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

// Convert Adapty event to Discord webhook
func adaptyToDiscord(event AdaptyEvent) DiscordWebhook {
	// Define colors for different event types
	colors := map[string]int{
		"subscription_started":    3066993,  // Green
		"subscription_renewed":    3447003,  // Blue
		"subscription_expired":    16776960, // Yellow
		"subscription_canceled":   15158332, // Red
		"trial_started":           10181046, // Purple
		"trial_converted":         3066993,  // Green
		"trial_expired":           15158332, // Red
		"transaction_completed":   3066993,  // Green
		"transaction_restored":    3447003,  // Blue
		"transaction_refunded":    15158332, // Red
	}

	// Default color if not found
	color := 7506394 // Gray

	if c, exists := colors[event.EventType]; exists {
		color = c
	}

	// Format the title based on event type
	title := fmt.Sprintf("Adapty: %s", formatEventType(event.EventType))
	
	// Initialize fields for the embed
	fields := []DiscordEmbedField{}
	
	// Add user information
	if event.Data.CustomerUserID != "" {
		fields = append(fields, DiscordEmbedField{
			Name:   "User",
			Value:  event.Data.CustomerUserID,
			Inline: true,
		})
	}
	
	fields = append(fields, DiscordEmbedField{
		Name:   "Profile ID",
		Value:  event.Data.ProfileID,
		Inline: true,
	})
	
	// Add subscription details if available
	if event.Data.Subscription != nil {
		fields = append(fields, DiscordEmbedField{
			Name:   "Product",
			Value:  event.Data.Subscription.ProductID,
			Inline: true,
		})
		
		fields = append(fields, DiscordEmbedField{
			Name:   "Status",
			Value:  event.Data.Subscription.Status,
			Inline: true,
		})
		
		fields = append(fields, DiscordEmbedField{
			Name:   "Store",
			Value:  event.Data.Subscription.Store,
			Inline: true,
		})
		
		if event.Data.Subscription.IsSandbox {
			fields = append(fields, DiscordEmbedField{
				Name:   "Environment",
				Value:  "Sandbox",
				Inline: true,
			})
		} else {
			fields = append(fields, DiscordEmbedField{
				Name:   "Environment",
				Value:  "Production",
				Inline: true,
			})
		}
		
		// Add date information if available
		if event.Data.Subscription.StartedAt > 0 {
			startedAt := time.Unix(event.Data.Subscription.StartedAt, 0)
			fields = append(fields, DiscordEmbedField{
				Name:   "Started At",
				Value:  startedAt.Format(time.RFC3339),
				Inline: true,
			})
		}
		
		if event.Data.Subscription.ExpiresAt > 0 {
			expiresAt := time.Unix(event.Data.Subscription.ExpiresAt, 0)
			fields = append(fields, DiscordEmbedField{
				Name:   "Expires At",
				Value:  expiresAt.Format(time.RFC3339),
				Inline: true,
			})
		}
	}
	
	// Add transaction details if available
	if event.Data.Transaction != nil {
		if event.Data.Transaction.ProductID != "" && (event.Data.Subscription == nil || event.Data.Subscription.ProductID == "") {
			fields = append(fields, DiscordEmbedField{
				Name:   "Product",
				Value:  event.Data.Transaction.ProductID,
				Inline: true,
			})
		}
		
		fields = append(fields, DiscordEmbedField{
			Name:   "Transaction ID",
			Value:  event.Data.Transaction.ID,
			Inline: true,
		})
		
		if event.Data.Transaction.Value > 0 {
			fields = append(fields, DiscordEmbedField{
				Name:   "Amount",
				Value:  fmt.Sprintf("%.2f %s", event.Data.Transaction.Value, event.Data.Transaction.Currency),
				Inline: true,
			})
		}
		
		if event.Data.Transaction.IsSandbox {
			fields = append(fields, DiscordEmbedField{
				Name:   "Environment",
				Value:  "Sandbox",
				Inline: true,
			})
		} else if event.Data.Subscription == nil {
			fields = append(fields, DiscordEmbedField{
				Name:   "Environment",
				Value:  "Production",
				Inline: true,
			})
		}
		
		if event.Data.Transaction.IsRestored {
			fields = append(fields, DiscordEmbedField{
				Name:   "Restored",
				Value:  "Yes",
				Inline: true,
			})
		}
	}

	// Create description based on event type
	description := createEventDescription(event)

	// Create the Discord webhook payload
	return DiscordWebhook{
		Username:  "Adapty",
		AvatarURL: "https://avatars.githubusercontent.com/u/55606573", // Adapty GitHub avatar
		Embeds: []DiscordEmbed{
			{
				Title:       title,
				Description: description,
				Color:       color,
				Fields:      fields,
				Timestamp:   time.Now().Format(time.RFC3339),
				Footer: &DiscordEmbedFooter{
					Text: "Adapty Subscription Management",
				},
			},
		},
	}
}

// formatEventType converts event_type to a more readable format
func formatEventType(eventType string) string {
	switch eventType {
	case "subscription_started":
		return "Subscription Started"
	case "subscription_renewed":
		return "Subscription Renewed"
	case "subscription_expired":
		return "Subscription Expired"
	case "subscription_canceled":
		return "Subscription Canceled"
	case "trial_started":
		return "Trial Started"
	case "trial_converted":
		return "Trial Converted"
	case "trial_expired":
		return "Trial Expired"
	case "transaction_completed":
		return "Transaction Completed"
	case "transaction_restored":
		return "Transaction Restored"
	case "transaction_refunded":
		return "Transaction Refunded"
	default:
		return eventType
	}
}

// createEventDescription generates a description based on the event type
func createEventDescription(event AdaptyEvent) string {
	switch event.EventType {
	case "subscription_started":
		return "A new subscription has been started."
	case "subscription_renewed":
		return "A subscription has been successfully renewed."
	case "subscription_expired":
		return "A subscription has expired."
	case "subscription_canceled":
		return "A subscription has been canceled."
	case "trial_started":
		return "A new trial period has started."
	case "trial_converted":
		return "A trial has been converted to a paid subscription."
	case "trial_expired":
		return "A trial period has expired."
	case "transaction_completed":
		return "A transaction has been completed successfully."
	case "transaction_restored":
		return "A transaction has been restored."
	case "transaction_refunded":
		return "A transaction has been refunded."
	default:
		return fmt.Sprintf("Received event: %s", event.EventType)
	}
}

// Determine payload type and process accordingly
func processPayload(body []byte) (DiscordWebhook, error) {
	// Try to unmarshal as GCP notification first
	var gcpNotification Notification
	gcpErr := json.Unmarshal(body, &gcpNotification)
	
	// Check if it looks like a valid GCP notification
	if gcpErr == nil && gcpNotification.Incident.IncidentID != "" {
		log.Println("Processing as GCP Monitoring notification")
		return gcpToDiscord(gcpNotification), nil
	}
	
	// Try to unmarshal as Adapty event
	var adaptyEvent AdaptyEvent
	adaptyErr := json.Unmarshal(body, &adaptyEvent)
	
	// Check if it looks like a valid Adapty event
	if adaptyErr == nil && (adaptyEvent.Event != "" || adaptyEvent.EventType != "") {
		log.Println("Processing as Adapty event")
		return adaptyToDiscord(adaptyEvent), nil
	}
	
	// If both failed, return the most informative error
	if gcpErr != nil && adaptyErr != nil {
		return DiscordWebhook{}, fmt.Errorf("failed to parse payload as either GCP or Adapty: %v, %v", gcpErr, adaptyErr)
	}
	
	return DiscordWebhook{}, fmt.Errorf("payload format not recognized")
}

// F is the Cloud Function entry point
func F(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request: Method=%s, ContentType=%s", r.Method, r.Header.Get("Content-Type"))

	// Get appropriate Discord webhook URL based on the event type
	var discordWebhookURL string
	
	// For GCP alerts
	gcpDiscordWebhookURL := os.Getenv("GCP_DISCORD_WEBHOOK_URL")
	if gcpDiscordWebhookURL == "" {
		log.Fatalln("`GCP_DISCORD_WEBHOOK_URL` is not set in the environment")
	}
	
	// For Adapty events  
	adaptyDiscordWebhookURL := os.Getenv("ADAPTY_DISCORD_WEBHOOK_URL")
	if adaptyDiscordWebhookURL == "" {
		log.Fatalln("`ADAPTY_DISCORD_WEBHOOK_URL` is not set in the environment")
	}

	// Validate Discord webhook URLs
	if _, err := url.Parse(gcpDiscordWebhookURL); err != nil {
		log.Fatalf("Invalid GCP Discord webhook URL: %v", err)
	}
	
	if _, err := url.Parse(adaptyDiscordWebhookURL); err != nil {
		log.Fatalf("Invalid Adapty Discord webhook URL: %v", err)
	}

	// Check for valid method and content type
	if contentType := r.Header.Get("Content-Type"); r.Method != "POST" || contentType != "application/json" {
		log.Printf("Invalid method / content-type: %s / %s", r.Method, contentType)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid request"))
		return
	}

	// Determine authentication method based on source
	isAuthenticated := false
	
	// Check for Adapty authorization header
	adaptyAuthToken := os.Getenv("ADAPTY_AUTH_TOKEN")
	if adaptyAuthToken != "" {
		receivedToken := r.Header.Get("Authorization")
		if receivedToken == adaptyAuthToken {
			isAuthenticated = true
			log.Println("Authenticated via Adapty Authorization header")
		}
	}
	
	// If not authenticated yet, check for GCP auth token in query params
	if !isAuthenticated {
		gcpAuthToken := os.Getenv("GCP_AUTH_TOKEN")
		if gcpAuthToken != "" {
			receivedToken := r.URL.Query().Get("auth_token")
			if receivedToken == gcpAuthToken {
				isAuthenticated = true
				log.Println("Authenticated via GCP auth_token query parameter")
			}
		}
	}
	
	// Deny access if not authenticated
	if !isAuthenticated {
		log.Println("Authentication failed")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
		return
	}

	// Read the request body
	var bodyBytes []byte
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("could not read request body"))
		return
	}
	defer r.Body.Close()
	
	// For debugging (commented out to avoid logging sensitive information)
	// log.Printf("Received payload: %s", string(bodyBytes))
	
	// Process the payload based on its type and determine which Discord webhook URL to use
	discordWebhook, err := processPayload(bodyBytes)
	if err != nil {
		log.Printf("Error processing payload: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid payload format"))
		return
	}
	
	// Choose the appropriate Discord webhook URL based on the username
	// This assumes that gcpToDiscord sets username to "GCP Monitoring" and 
	// adaptyToDiscord sets username to "Adapty"
	if discordWebhook.Username == "GCP Monitoring" {
		discordWebhookURL = gcpDiscordWebhookURL
		log.Println("Using GCP Discord webhook URL")
	} else if discordWebhook.Username == "Adapty" {
		discordWebhookURL = adaptyDiscordWebhookURL
		log.Println("Using Adapty Discord webhook URL")
	} else {
		log.Printf("Unrecognized webhook username: %s", discordWebhook.Username)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Marshal the Discord webhook payload
	payload, err := json.Marshal(discordWebhook)
	if err != nil {
		log.Printf("Error marshaling discord webhook: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Send to Discord
	res, err := http.Post(discordWebhookURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("Error posting to discord: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	// Check the Discord response
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("Unexpected Discord response status: %d", res.StatusCode)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully sent to Discord with status: %d", res.StatusCode)

	// Return the Discord webhook payload as the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(discordWebhook); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}