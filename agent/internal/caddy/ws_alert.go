package caddy

import (
	"encoding/json"
)

// WsAlertReporter sends certificate alerts via WebSocket.
type WsAlertReporter struct {
	SendFunc func(v interface{}) error
}

// SendCertificateAlert sends a certificate alert to the control plane.
func (r *WsAlertReporter) SendCertificateAlert(alert CertificateAlert) error {
	msg := map[string]interface{}{
		"type": "certificate_alert",
		"payload": alert,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return r.SendFunc(data)
}
