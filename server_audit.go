package skillsdex

import (
	"encoding/json"
	"log"
	"net/http"
)

func (a *app) audit(r *http.Request, actor *User, action, targetType, targetID, targetLabel string, details map[string]any) {
	detailJSON := "{}"
	if len(details) > 0 {
		if raw, err := json.Marshal(details); err == nil {
			detailJSON = string(raw)
		}
	}
	event := AuditEvent{
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		TargetLabel: targetLabel,
		DetailJSON:  detailJSON,
		RemoteAddr:  a.remoteAddr(r),
		UserAgent:   r.UserAgent(),
	}
	if actor != nil {
		event.ActorUserID = actor.ID
		event.ActorUsername = actor.Username
	}
	if err := createAuditEvent(r.Context(), a.db, event); err != nil {
		log.Printf("audit event failed: %v", err)
	}
}
