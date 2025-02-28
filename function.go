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

// Process Adapty event using flexible map approach
func adaptyMapToDiscord(event map[string]interface{}) DiscordWebhook {
	// Define colors for different event types
	colors := map[string]int{
		// Existing events
		"subscription_started":           3066993,  // Green
		"subscription_renewed":           3447003,  // Blue
		"subscription_expired":           16776960, // Yellow
		"subscription_canceled":          15158332, // Red
		"trial_started":                  10181046, // Purple
		"trial_converted":                3066993,  // Green
		"trial_expired":                  15158332, // Red
		"non_subscription_purchase":      3066993,  // Green
		"subscription_in_grace_period":   16776960, // Yellow
		"transaction_refunded":           15158332, // Red
		
		// Added missing events
		"subscription_renewal_cancelled": 15158332, // Red
		"subscription_renewal_reactivated": 3066993, // Green
		"subscription_paused":            16776960, // Yellow
		"subscription_deferred":          16776960, // Yellow
		"trial_renewal_cancelled":        15158332, // Red
		"trial_renewal_reactivated":      3066993,  // Green
		"entered_grace_period":           16776960, // Yellow
		"billing_issue_detected":         15158332, // Red
		"subscription_refunded":          15158332, // Red
		"non_subscription_purchase_refunded": 15158332, // Red
		"access_level_updated":           3447003,  // Blue
	}

	// Default color if not found
	color := 7506394 // Gray
	
	// Get event type
	eventType := ""
	if et, ok := event["event_type"].(string); ok {
		eventType = et
	}
	
	if c, exists := colors[eventType]; exists {
		color = c
	}

	// Format the title based on event type
	title := fmt.Sprintf("Adapty: %s", formatEventType(eventType))
	
	// Initialize fields for the embed
	fields := []DiscordEmbedField{}
	
	// Add user information
	if customerUserID, ok := event["customer_user_id"].(string); ok && customerUserID != "" {
		fields = append(fields, DiscordEmbedField{
			Name:   "User",
			Value:  customerUserID,
			Inline: true,
		})
	}
	
	if email, ok := event["email"].(string); ok && email != "" {
		fields = append(fields, DiscordEmbedField{
			Name:   "Email",
			Value:  email,
			Inline: true,
		})
	}
	
	if profileID, ok := event["profile_id"].(string); ok && profileID != "" {
		fields = append(fields, DiscordEmbedField{
			Name:   "Profile ID",
			Value:  profileID,
			Inline: true,
		})
	}
	
	// Extract event properties
	eventProps, hasEventProps := event["event_properties"].(map[string]interface{})
	if hasEventProps {
		// Add transaction details
		if txnID, ok := eventProps["transaction_id"].(string); ok && txnID != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Transaction ID",
				Value:  txnID,
				Inline: true,
			})
		}
		
		// Add product details
		if productID, ok := eventProps["vendor_product_id"].(string); ok && productID != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Product",
				Value:  productID,
				Inline: true,
			})
		}
		
		// Add base plan for Google Play
		if basePlanID, ok := eventProps["base_plan_id"].(string); ok && basePlanID != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Base Plan",
				Value:  basePlanID,
				Inline: true,
			})
		}
		
		// Add store information
		if store, ok := eventProps["store"].(string); ok && store != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Store",
				Value:  store,
				Inline: true,
			})
		}
		
		// Add environment (sandbox/production)
		if env, ok := eventProps["environment"].(string); ok && env != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Environment",
				Value:  env,
				Inline: true,
			})
		}
		
		// Add price/revenue information if available
		if priceUSDFloat, ok := eventProps["price_usd"].(float64); ok {
			value := fmt.Sprintf("$%.2f", priceUSDFloat)
			if proceedsUSDFloat, ok := eventProps["proceeds_usd"].(float64); ok {
				value += fmt.Sprintf(" (Net: $%.2f)", proceedsUSDFloat)
			}
			fields = append(fields, DiscordEmbedField{
				Name:   "Revenue (USD)",
				Value:  value,
				Inline: true,
			})
		}
		
		// Add subscription status information
		if hasAccessBool, ok := eventProps["profile_has_access_level"].(bool); ok {
			accessStatus := "No"
			if hasAccessBool {
				accessStatus = "Yes"
			}
			fields = append(fields, DiscordEmbedField{
				Name:   "Has Access",
				Value:  accessStatus,
				Inline: true,
			})
		}
		
		// Add renewal status
		if willRenewBool, ok := eventProps["will_renew"].(bool); ok {
			renewStatus := "No"
			if willRenewBool {
				renewStatus = "Yes"
			}
			fields = append(fields, DiscordEmbedField{
				Name:   "Will Renew",
				Value:  renewStatus,
				Inline: true,
			})
		}
		
		// Handle date information
		if purchaseDate, ok := eventProps["purchase_date"].(string); ok && purchaseDate != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Purchase Date",
				Value:  formatDate(purchaseDate),
				Inline: true,
			})
		}
		
		if expiresAt, ok := eventProps["subscription_expires_at"].(string); ok && expiresAt != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Expires At",
				Value:  formatDate(expiresAt),
				Inline: true,
			})
		}
		
		// For pause events, show the pause start date
		if pauseStartDate, ok := eventProps["pause_start_date"].(string); ok && pauseStartDate != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Pause Start Date",
				Value:  formatDate(pauseStartDate),
				Inline: true,
			})
		}
		
		// For pause events, show the auto-resume date if available
		if autoResumeDate, ok := eventProps["auto_resume_date"].(string); ok && autoResumeDate != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Auto Resume Date",
				Value:  formatDate(autoResumeDate),
				Inline: true,
			})
		}
		
		// For defer events, show the new expiration date
		if deferredExpDate, ok := eventProps["deferred_expiration_date"].(string); ok && deferredExpDate != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Deferred Expiration",
				Value:  formatDate(deferredExpDate),
				Inline: true,
			})
		}
		
		// For cancellations, show the reason if available
		if reason, ok := eventProps["cancellation_reason"].(string); ok && reason != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Cancellation Reason",
				Value:  reason,
				Inline: true,
			})
		}
		
		// For billing issues, show error details if available
		if billingError, ok := eventProps["billing_error"].(string); ok && billingError != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Billing Error",
				Value:  billingError,
				Inline: true,
			})
		}
		
		// For access level updates, show the access level details
		if accessLevel, ok := eventProps["access_level"].(string); ok && accessLevel != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Access Level",
				Value:  accessLevel,
				Inline: true,
			})
		}
		
		// Show paywall information if available
		if paywallName, ok := eventProps["paywall_name"].(string); ok && paywallName != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "Paywall",
				Value:  paywallName,
				Inline: true,
			})
		}
		
		// Show A/B test information if available
		if abTestName, ok := eventProps["ab_test_name"].(string); ok && abTestName != "" {
			fields = append(fields, DiscordEmbedField{
				Name:   "A/B Test",
				Value:  abTestName,
				Inline: true,
			})
		}
	}
	
	// Create description based on event type
	description := createMapEventDescription(eventType, eventProps)

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

// createMapEventDescription generates a description based on the event type
func createMapEventDescription(eventType string, eventProps map[string]interface{}) string {
	// Handle basic descriptions for common events
	switch eventType {
	// Existing events
	case "subscription_started":
		return "A new subscription has been started."
	case "subscription_renewed":
		return "A subscription has been successfully renewed."
	case "subscription_expired":
		return "A subscription has expired."
	case "subscription_canceled":
		return "A subscription has been canceled."
	case "subscription_in_grace_period":
		return "A subscription is in grace period due to billing issues."
	case "trial_started":
		return "A new trial period has started."
	case "trial_converted":
		return "A trial has been converted to a paid subscription."
	case "trial_expired":
		return "A trial period has expired."
	case "non_subscription_purchase":
		return "A one-time purchase has been completed."
	case "transaction_refunded":
		return "A transaction has been refunded."
		
	// Added events
	case "subscription_renewal_cancelled":
		return "Auto-renewal for a subscription has been cancelled."
	case "subscription_renewal_reactivated":
		return "Auto-renewal for a subscription has been reactivated."
	case "subscription_paused":
		return "A subscription has been paused."
	case "subscription_deferred":
		return "A subscription renewal has been deferred to a later date."
	case "trial_renewal_cancelled":
		return "Auto-renewal for a trial has been cancelled."
	case "trial_renewal_reactivated":
		return "Auto-renewal for a trial has been reactivated."
	case "entered_grace_period":
		return "A subscription has entered a grace period due to billing issues."
	case "billing_issue_detected":
		return "A billing issue has been detected for a subscription."
	case "subscription_refunded":
		return "A subscription payment has been refunded."
	case "non_subscription_purchase_refunded":
		return "A one-time purchase has been refunded."
	case "access_level_updated":
		return "A user's access level has been updated."
	}
	
	// Enhanced descriptions with more details when available
	if eventProps != nil {
		// Get the product name if available
		productID := ""
		if id, ok := eventProps["vendor_product_id"].(string); ok && id != "" {
			productID = id
		}
		
		// Include product ID in descriptions if available
		if productID != "" {
			switch eventType {
			// Existing events with product ID
			case "subscription_started":
				return fmt.Sprintf("A new subscription for **%s** has been started.", productID)
			case "subscription_renewed":
				return fmt.Sprintf("The subscription for **%s** has been successfully renewed.", productID)
			case "subscription_expired":
				return fmt.Sprintf("The subscription for **%s** has expired.", productID)
			case "subscription_canceled":
				return fmt.Sprintf("The subscription for **%s** has been canceled.", productID)
			case "subscription_in_grace_period":
				return fmt.Sprintf("The subscription for **%s** is in grace period due to billing issues.", productID)
			case "trial_started":
				return fmt.Sprintf("A new trial period for **%s** has started.", productID)
			case "trial_converted":
				return fmt.Sprintf("The trial for **%s** has been converted to a paid subscription.", productID)
			case "trial_expired":
				return fmt.Sprintf("The trial period for **%s** has expired.", productID)
			case "non_subscription_purchase":
				return fmt.Sprintf("A one-time purchase of **%s** has been completed.", productID)
			case "transaction_refunded":
				return fmt.Sprintf("A transaction for **%s** has been refunded.", productID)
				
			// New events with product ID
			case "subscription_renewal_cancelled":
				return fmt.Sprintf("Auto-renewal for the **%s** subscription has been cancelled.", productID)
			case "subscription_renewal_reactivated":
				return fmt.Sprintf("Auto-renewal for the **%s** subscription has been reactivated.", productID)
			case "subscription_paused":
				return fmt.Sprintf("The subscription for **%s** has been paused.", productID)
			case "subscription_deferred":
				return fmt.Sprintf("The renewal for the **%s** subscription has been deferred to a later date.", productID)
			case "trial_renewal_cancelled":
				return fmt.Sprintf("Auto-renewal for the **%s** trial has been cancelled.", productID)
			case "trial_renewal_reactivated":
				return fmt.Sprintf("Auto-renewal for the **%s** trial has been reactivated.", productID)
			case "entered_grace_period":
				return fmt.Sprintf("The subscription for **%s** has entered a grace period due to billing issues.", productID)
			case "billing_issue_detected":
				return fmt.Sprintf("A billing issue has been detected for the **%s** subscription.", productID)
			case "subscription_refunded":
				return fmt.Sprintf("A subscription payment for **%s** has been refunded.", productID)
			case "non_subscription_purchase_refunded":
				return fmt.Sprintf("A one-time purchase of **%s** has been refunded.", productID)
			case "access_level_updated":
				return fmt.Sprintf("Access level related to **%s** has been updated.", productID)
			}
		}
		
		// Include price in descriptions for purchases if available
		if priceUSDFloat, ok := eventProps["price_usd"].(float64); ok {
			switch eventType {
			case "subscription_started":
				return fmt.Sprintf("A new subscription has been started for $%.2f.", priceUSDFloat)
			case "non_subscription_purchase":
				return fmt.Sprintf("A one-time purchase has been completed for $%.2f.", priceUSDFloat)
			case "subscription_refunded":
				return fmt.Sprintf("A subscription payment of $%.2f has been refunded.", priceUSDFloat)
			case "non_subscription_purchase_refunded":
				return fmt.Sprintf("A one-time purchase of $%.2f has been refunded.", priceUSDFloat)
			}
		}
		
		// Add specific descriptions for access level updates
		if eventType == "access_level_updated" {
			if accessLevel, ok := eventProps["access_level"].(string); ok && accessLevel != "" {
				return fmt.Sprintf("User access level has been updated to **%s**.", accessLevel)
			}
		}
	}
	
	// For any unhandled event type
	return fmt.Sprintf("Received event: %s", eventType)
}

// formatEventType converts event_type to a more readable format
func formatEventType(eventType string) string {
	switch eventType {
	// Existing event types
	case "subscription_started":
		return "Subscription Started"
	case "subscription_renewed":
		return "Subscription Renewed"
	case "subscription_expired":
		return "Subscription Expired"
	case "subscription_canceled":
		return "Subscription Canceled"
	case "subscription_in_grace_period":
		return "Subscription in Grace Period"
	case "trial_started":
		return "Trial Started"
	case "trial_converted":
		return "Trial Converted"
	case "trial_expired":
		return "Trial Expired"
	case "non_subscription_purchase":
		return "One-time Purchase"
	case "transaction_refunded":
		return "Transaction Refunded"
		
	// New event types
	case "subscription_renewal_cancelled":
		return "Subscription Renewal Cancelled"
	case "subscription_renewal_reactivated":
		return "Subscription Renewal Reactivated"
	case "subscription_paused":
		return "Subscription Paused"
	case "subscription_deferred":
		return "Subscription Deferred"
	case "trial_renewal_cancelled":
		return "Trial Renewal Cancelled"
	case "trial_renewal_reactivated":
		return "Trial Renewal Reactivated"
	case "entered_grace_period":
		return "Entered Grace Period"
	case "billing_issue_detected":
		return "Billing Issue Detected"
	case "subscription_refunded":
		return "Subscription Refunded"
	case "non_subscription_purchase_refunded":
		return "One-time Purchase Refunded"
	case "access_level_updated":
		return "Access Level Updated"
		
	default:
		// Capitalize and space out the event type
		parts := strings.Split(eventType, "_")
		for i, part := range parts {
			if len(part) > 0 {
				parts[i] = strings.ToUpper(part[0:1]) + part[1:]
			}
		}
		return strings.Join(parts, " ")
	}
}

// formatDate makes the ISO date more readable
func formatDate(isoDate string) string {
	// Try to parse the date string
	t, err := time.Parse("2006-01-02T15:04:05.000000-0700", isoDate)
	if err != nil {
		// Try alternate format
		t, err = time.Parse(time.RFC3339, isoDate)
		if err != nil {
			// If parsing fails, return the original string
			return isoDate
		}
	}
	
	// Format to a more readable form
	return t.Format("Jan 2, 2006 15:04 MST")
}

func processPayload(body []byte) (DiscordWebhook, error) {
	// First, try a generic unmarshal to detect what kind of payload this is
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(body, &rawPayload); err != nil {
		return DiscordWebhook{}, fmt.Errorf("failed to parse JSON payload: %v", err)
	}
	
	// Try to unmarshal as GCP notification
	var gcpNotification Notification
	gcpErr := json.Unmarshal(body, &gcpNotification)
	
	// Check if it looks like a valid GCP notification
	if gcpErr == nil && gcpNotification.Incident.IncidentID != "" {
		log.Println("Processing as GCP Monitoring notification")
		return gcpToDiscord(gcpNotification), nil
	}

	// Process as Adapty webhook event
	adaptyEvent := make(map[string]interface{})
	if err := json.Unmarshal(body, &adaptyEvent); err == nil {
		log.Println("Processing as Adapty event (map format)")
		return adaptyMapToDiscord(adaptyEvent), nil
	}
	
	// If payload doesn't match our expected structures but has some Adapty-like fields,
	// try to format it as best we can
	if event, hasEvent := rawPayload["event"].(string); hasEvent {
		log.Printf("Detected partial Adapty event with event: %s", event)
		
		// Create a basic Discord webhook for this event
		return DiscordWebhook{
			Username:  "Adapty",
			AvatarURL: "https://avatars.githubusercontent.com/u/55606573",
			Embeds: []DiscordEmbed{
				{
					Title:       fmt.Sprintf("Adapty: %s", event),
					Description: fmt.Sprintf("Received event: %s", event),
					Color:       7506394, // Gray
					Timestamp:   time.Now().Format(time.RFC3339),
					Footer: &DiscordEmbedFooter{
						Text: "Adapty Subscription Management",
					},
				},
			},
		}, nil
	}
	
	// If both failed, return an informative error
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
	
	// For debugging - log the incoming payload (commented out for production)
	log.Printf("Received payload: %s", string(bodyBytes))
	
	// Special handling for Adapty verification tests
	var rawPayload map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawPayload); err == nil {
		// Check for Adapty's adapty_check field (their actual verification format)
		if checkString, hasAdaptyCheck := rawPayload["adapty_check"].(string); hasAdaptyCheck {
			log.Println("Detected Adapty verification check")
			
			// Return the exact response format Adapty expects with the same check string
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			response := fmt.Sprintf(`{"adapty_check_response": "%s"}`, checkString)
			w.Write([]byte(response))
			return
		}
		
		// Check if it's a direct isMount event (alternative check format)
		if event, ok := rawPayload["event"].(string); ok && event == "isMount" {
			log.Println("Detected Adapty isMount test event")
			
			// For isMount test events, just return 200 OK
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","message":"Webhook verification successful"}`))
			return
		}
		
		// Also handle the case where there's a nested data structure
		if data, ok := rawPayload["data"].(map[string]interface{}); ok {
			if event, ok := data["event"].(string); ok && event == "isMount" {
				log.Println("Detected nested Adapty isMount test event")
				
				// For isMount test events, just return 200 OK
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status":"ok","message":"Webhook verification successful"}`))
				return
			}
		}
	}
	
	// If we're here, we'll proceed with normal processing
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