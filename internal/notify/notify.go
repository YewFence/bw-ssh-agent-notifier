package notify

import (
	"context"
	"errors"
	"time"

	"github.com/godbus/dbus/v5"
)

type Notification struct {
	Summary string
	Body    string
}

type Notifier interface {
	Send(context.Context, Notification) error
}

type noopNotifier struct{}

func Noop() Notifier {
	return noopNotifier{}
}

func (noopNotifier) Send(context.Context, Notification) error {
	return nil
}

type DBusNotifier struct {
	AppName       string
	CallTimeout   time.Duration
	ExpireTimeout time.Duration
}

func (notifier DBusNotifier) Send(ctx context.Context, notification Notification) error {
	if notification.Summary == "" {
		return errors.New("notification summary is required")
	}
	callTimeout := notifier.CallTimeout
	if callTimeout <= 0 {
		callTimeout = 2 * time.Second
	}
	expireTimeout := notifier.ExpireTimeout
	if expireTimeout <= 0 {
		expireTimeout = 2 * time.Second
	}
	appName := notifier.AppName
	if appName == "" {
		appName = "Bitwarden SSH Agent Notifier"
	}

	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	conn, err := dbus.SessionBusPrivate(dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()
	if err := conn.Auth(nil); err != nil {
		return err
	}
	if err := conn.Hello(); err != nil {
		return err
	}

	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.CallWithContext(
		ctx,
		"org.freedesktop.Notifications.Notify",
		0,
		appName,
		uint32(0),
		"",
		notification.Summary,
		notification.Body,
		[]string{},
		map[string]dbus.Variant{},
		int32(expireTimeout/time.Millisecond),
	)
	return call.Err
}

func CheckDBus(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := dbus.SessionBusPrivate(dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()
	if err := conn.Auth(nil); err != nil {
		return err
	}
	if err := conn.Hello(); err != nil {
		return err
	}

	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.CallWithContext(ctx, "org.freedesktop.Notifications.GetServerInformation", 0)
	return call.Err
}
