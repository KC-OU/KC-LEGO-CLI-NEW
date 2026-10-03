package botapi

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// Discord interaction/response type numbers this package needs — see
// https://discord.com/developers/docs/interactions/receiving-and-responding.
const (
	discordTypePing                     = 1
	discordTypeApplicationCommand       = 2
	discordTypePong                     = 1
	discordTypeChannelMessageWithSource = 4
)

// verifyDiscordSignature checks the Ed25519 signature Discord puts on every
// Interactions Endpoint request (headers X-Signature-Ed25519 and
// X-Signature-Timestamp, signing timestamp+body) — this is the *only* proof
// of authenticity an internet-facing endpoint like this one gets, unlike
// /bot/action's loopback-only network trust. publicKeyHex is the
// application's Public Key from Discord's Developer Portal.
func verifyDiscordSignature(publicKeyHex, timestamp, signatureHex string, body []byte) bool {
	pubKey, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(signatureHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	msg := append([]byte(timestamp), body...)
	return ed25519.Verify(ed25519.PublicKey(pubKey), msg, sig)
}

// discordOption is one slash-command argument, or — one level up — a
// subcommand itself, which Discord shapes the same way with its own nested
// Options instead of a Value. The /wms command only ever nests one level
// (subcommand -> its own plain arguments), matching the same one-level rule
// this project already applies to menu sub-menus (access.go's
// validateMenuLayouts).
type discordOption struct {
	Name    string          `json:"name"`
	Value   json.RawMessage `json:"value"`
	Options []discordOption `json:"options"`
}

type discordInteraction struct {
	Type   int `json:"type"`
	Member struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member"`
	Data struct {
		Name    string          `json:"name"`
		Options []discordOption `json:"options"`
	} `json:"data"`
}

func optString(opts []discordOption, name string) string {
	for _, o := range opts {
		if o.Name == name {
			var s string
			_ = json.Unmarshal(o.Value, &s)
			return s
		}
	}
	return ""
}

func optInt64(opts []discordOption, name string) int64 {
	for _, o := range opts {
		if o.Name == name {
			var n int64
			_ = json.Unmarshal(o.Value, &n)
			return n
		}
	}
	return 0
}

// interactionReply wraps content as Discord's required response envelope.
// Every reply here is ephemeral (flags: 64 — visible only to whoever ran the
// command), success or error alike: a bot action's result isn't meant for
// the whole channel, the same instinct as /bot/action collapsing unknown-ID
// and wrong-PIN into one 401 rather than confirming anything to onlookers.
func interactionReply(content string) map[string]any {
	return map[string]any{
		"type": discordTypeChannelMessageWithSource,
		"data": map[string]any{"content": content, "flags": 64},
	}
}

// InteractionsHandler serves Discord's single Interactions Endpoint URL: the
// no-n8n path to the same four bot actions as /bot/action, triggered by a
// "/wms" slash command's subcommands (reassign/message/kick/tickets/alerts)
// instead of an n8n workflow deciding what to call. It shares Dispatch with
// /bot/action, so neither path can do anything the other can't.
//
// Unlike /bot/action, this is meant to be mounted on the *public* web
// gateway (internal/gateway), not the loopback-only bot API — Discord's own
// servers have to reach it from the internet. That's safe only because
// Discord signs every request; verifyDiscordSignature is the entire trust
// boundary here, replacing the network-location trust /bot/action relies on.
func InteractionsHandler(s *Server, publicKeyHex string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !verifyDiscordSignature(publicKeyHex, r.Header.Get("X-Signature-Timestamp"), r.Header.Get("X-Signature-Ed25519"), body) {
			http.Error(w, "invalid request signature", http.StatusUnauthorized)
			return
		}
		var in discordInteraction
		if err := json.Unmarshal(body, &in); err != nil {
			http.Error(w, "malformed interaction", http.StatusBadRequest)
			return
		}
		if in.Type == discordTypePing {
			writeJSON(w, http.StatusOK, map[string]any{"type": discordTypePong})
			return
		}
		if in.Type != discordTypeApplicationCommand || in.Data.Name != "wms" || len(in.Data.Options) != 1 {
			writeJSON(w, http.StatusOK, interactionReply("Unknown command."))
			return
		}
		sub := in.Data.Options[0] // the one subcommand actually used: reassign / message / kick / tickets / alerts
		opts := sub.Options
		req := ActionRequest{Platform: "discord", ID: in.Member.User.ID, PIN: optString(opts, "pin")}
		switch sub.Name {
		case "reassign":
			req.Action = "reassign_ticket"
			req.Params, _ = json.Marshal(reassignParams{TicketID: optInt64(opts, "ticket_id"), To: optString(opts, "to")})
		case "message":
			req.Action = "message_user"
			req.Params, _ = json.Marshal(messageParams{Username: optString(opts, "username"), Body: optString(opts, "body")})
		case "kick":
			req.Action = "kick_session"
			req.Params, _ = json.Marshal(kickParams{SessionID: optString(opts, "session_id"), Message: optString(opts, "message")})
		case "tickets":
			req.Action = "list_open_tickets"
		case "alerts":
			req.Action = "list_alerts"
		default:
			writeJSON(w, http.StatusOK, interactionReply("Unknown command."))
			return
		}
		status, result := s.Dispatch(req)
		writeJSON(w, http.StatusOK, interactionReply(describeResult(sub.Name, status, result)))
	}
}

func describeResult(action string, status int, result map[string]any) string {
	if status != http.StatusOK {
		if msg, ok := result["error"].(string); ok {
			return "❌ " + msg
		}
		return "❌ something went wrong"
	}
	switch action {
	case "reassign":
		return "✅ " + fmt.Sprint(result["detail"])
	case "message":
		return "✅ message sent"
	case "kick":
		return "✅ session kicked"
	case "tickets":
		n := 0
		if tickets, ok := result["tickets"].([]lego.Ticket); ok {
			n = len(tickets)
		}
		return fmt.Sprintf("✅ %d open ticket(s) — see Admin → Assign Work for details", n)
	case "alerts":
		n := 0
		if alerts, ok := result["alerts"].([]lego.AccuracyEscalation); ok {
			n = len(alerts)
		}
		return fmt.Sprintf("✅ %d open alert(s) — see Admin → Alerts for details", n)
	}
	return "✅ done"
}
