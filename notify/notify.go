// Package notify posts desktop notifications and plays alert sounds. Delivery
// uses the session bus / notify-send / kdialog on Linux, UserNotification on
// macOS, and a shell notification icon on Windows; unsupported environments
// report ErrUnsupported or ErrUnavailable rather than failing silently.
package notify

import "errors"

// ErrUnsupported is returned when the platform has no notification mechanism.
var ErrUnsupported = errors.New("notify: not supported on this platform")

// ErrUnavailable is returned when the platform supports notifications but this
// process/environment cannot show them (macOS needs a bundled .app the user has
// granted permission; Linux needs a session bus or a notify-send/kdialog CLI).
var ErrUnavailable = errors.New("notify: notifications unavailable in this process/environment (macOS: needs a bundled .app the user granted Notification permission; Linux: no session bus or notify-send/kdialog)")

// DefaultFreq is the default tone frequency (Hz) for Beep.
var DefaultFreq = 440.0

// DefaultDuration is the default tone duration (ms) for Beep.
var DefaultDuration = 200

// Urgency is the notification's importance hint.
type Urgency uint8

const (
	// UrgencyLow hints that the notification is informational.
	UrgencyLow Urgency = iota + 1

	// UrgencyNormal is the default urgency.
	UrgencyNormal

	// UrgencyCritical hints that the notification needs attention.
	UrgencyCritical
)

func (u Urgency) level() int {
	switch u {
	case UrgencyLow:
		return 0
	case UrgencyCritical:
		return 2
	default:
		return 1
	}
}

// Options customises a notification. Set at most one of Icon (a path) or
// IconData (raw PNG/JPEG bytes).
type Options struct {
	Icon string

	IconData []byte

	Urgency Urgency
}

func (o Options) validate() error {
	if o.Icon != "" && len(o.IconData) > 0 {
		return errors.New("notify: set at most one of Options.Icon and Options.IconData")
	}
	if o.IconData != nil && len(o.IconData) == 0 {
		return errors.New("notify: Options.IconData must not be empty")
	}
	return nil
}

// Show posts a notification with default options.
func Show(name, title, message string) error {
	return ShowOpts(name, title, message, Options{})
}

// ShowOpts posts a notification; name is the application name shown to the user.
func ShowOpts(name, title, message string, opts Options) error {
	if err := opts.validate(); err != nil {
		return err
	}
	return show(name, title, message, opts)
}
