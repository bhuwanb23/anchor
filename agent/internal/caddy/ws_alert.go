package caddy

import (
	"encoding/json"
)

// WsAlertReporter sends alerts via WebSocket.
type WsAlertReporter struct {
	SendFunc func(v interface{}) error
}

// SendCertificateAlert sends a certificate alert to the control plane.
func (r *WsAlertReporter) SendCertificateAlert(alert CertificateAlert) error {
	msg := map[string]interface{}{
		"type":    "certificate_alert",
		"payload": alert,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return r.SendFunc(data)
}

// SendErrorAlert sends an error alert to the control plane.
func (r *WsAlertReporter) SendErrorAlert(alert ErrorAlert) error {
	msg := map[string]interface{}{
		"type":    "error_alert",
		"payload": alert,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return r.SendFunc(data)
}

// SendServerEvent sends a server event to the control plane.
func (r *WsAlertReporter) SendServerEvent(event ServerEvent) error {
	msg := map[string]interface{}{
		"type":    "server_event",
		"payload": event,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return r.SendFunc(data)
}
