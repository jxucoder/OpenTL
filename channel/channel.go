// Package channel defines the Channel interface for TeleCoder input/output transports.
package channel

import "context"

// Channel represents an input/output transport (Slack, Telegram, etc.).
type Channel interface {
	Name() string
	Run(ctx context.Context) error
}
