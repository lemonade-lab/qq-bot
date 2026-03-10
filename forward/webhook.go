package forward

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// WebhookPayload is the incoming QQ Bot webhook request body.
type WebhookPayload struct {
	Op int             `json:"op"`
	ID string          `json:"id,omitempty"`
	T  string          `json:"t,omitempty"`
	D  json.RawMessage `json:"d"`
}

// ValidationChallenge is the data for op=13.
type ValidationChallenge struct {
	EventTs    string `json:"event_ts"`
	PlainToken string `json:"plain_token"`
}

// HandleWebhook handles POST /webhook/:app_id.
func HandleWebhook(store *BotStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		appID := c.Param("app_id")

		bot := store.Get(appID)
		if bot == nil {
			log.Printf("[webhook] bot %s not registered", appID)
			c.JSON(http.StatusNotFound, gin.H{"msg": "bot not registered"})

			return
		}

		rawBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "cannot read body"})

			return
		}

		// Verify Ed25519 signature (if bot has keys)
		sign := c.GetHeader("X-Signature-Ed25519")
		timestamp := c.GetHeader("X-Signature-Timestamp")

		if bot.HasKeys() {
			if sign == "" || timestamp == "" {
				log.Printf("[webhook] bot=%s missing signature headers", appID)
				c.JSON(http.StatusBadRequest, gin.H{"msg": "missing signature"})

				return
			}

			if !VerifySignature(bot.PubKey, timestamp, string(rawBody), sign) {
				log.Printf("[webhook] bot=%s invalid signature", appID)
				c.JSON(http.StatusBadRequest, gin.H{"msg": "invalid signature"})

				return
			}
		} else {
			log.Printf("[webhook] bot=%s no Ed25519 keys, skipping signature verification", appID)
		}

		var payload WebhookPayload
		if err := json.Unmarshal(rawBody, &payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "invalid json"})

			return
		}

		switch payload.Op {
		case 13:
			if !bot.HasKeys() {
				c.JSON(http.StatusInternalServerError, gin.H{"msg": "bot has no signing keys"})

				return
			}

			var challenge ValidationChallenge
			if err := json.Unmarshal(payload.D, &challenge); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"msg": "invalid challenge data"})

				return
			}

			signature := SignChallenge(bot.PrivKey, challenge.EventTs, challenge.PlainToken)
			log.Printf("[webhook] bot=%s op=13 challenge", appID)

			c.JSON(http.StatusOK, gin.H{
				"plain_token": challenge.PlainToken,
				"signature":   signature,
			})

		case 0:
			log.Printf("[webhook] bot=%s op=0 event=%s clients=%d", appID, payload.T, bot.ClientCount())
			bot.ForwardEvent(payload)
			c.Status(http.StatusNoContent)

		default:
			log.Printf("[webhook] bot=%s unknown op=%d", appID, payload.Op)
			c.Status(http.StatusNoContent)
		}
	}
}
