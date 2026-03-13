package platform

/*
#cgo LDFLAGS: -framework SystemConfiguration
#include <SystemConfiguration/SystemConfiguration.h>

// Callback bridge
extern void goNetworkCallback();

static SCDynamicStoreRef store;
static CFRunLoopSourceRef rlSource;

static void networkCallbackC(SCDynamicStoreRef store, CFArrayRef changedKeys, void *info) {
    goNetworkCallback();
}

static int setupNetworkNotifications() {
    SCDynamicStoreContext ctx = {0, NULL, NULL, NULL, NULL};
    store = SCDynamicStoreCreate(NULL, CFSTR("apex-agent"), networkCallbackC, &ctx);
    if (!store) return -1;

    CFStringRef keys[2] = {
        CFSTR("State:/Network/Global/IPv4"),
        CFSTR("State:/Network/Global/IPv6")
    };
    CFArrayRef watchedKeys = CFArrayCreate(NULL, (const void **)keys, 2, &kCFTypeArrayCallBacks);
    SCDynamicStoreSetNotificationKeys(store, watchedKeys, NULL);
    CFRelease(watchedKeys);

    rlSource = SCDynamicStoreCreateRunLoopSource(NULL, store, 0);
    CFRunLoopAddSource(CFRunLoopGetCurrent(), rlSource, kCFRunLoopDefaultMode);
    return 0;
}

static void runNetworkLoop() {
    CFRunLoopRun();
}

static void stopNetworkLoop() {
    CFRunLoopStop(CFRunLoopGetCurrent());
}
*/
import "C"

import (
	"context"
	"log/slog"
)

var networkChan chan struct{}

//export goNetworkCallback
func goNetworkCallback() {
	if networkChan != nil {
		select {
		case networkChan <- struct{}{}:
		default:
		}
	}
}

// NetworkMonitor watches for network changes.
type NetworkMonitor struct {
	log    *slog.Logger
	Events chan struct{}
}

func NewNetworkMonitor(log *slog.Logger) *NetworkMonitor {
	ch := make(chan struct{}, 10)
	networkChan = ch
	return &NetworkMonitor{
		log:    log.With("component", "network"),
		Events: ch,
	}
}

// Run starts the network change listener.
func (n *NetworkMonitor) Run(ctx context.Context) error {
	if C.setupNetworkNotifications() != 0 {
		return &networkError{"failed to set up network notifications"}
	}

	n.log.Info("network monitor started")

	done := make(chan struct{})
	go func() {
		C.runNetworkLoop()
		close(done)
	}()

	select {
	case <-ctx.Done():
		C.stopNetworkLoop()
		<-done
		return ctx.Err()
	case <-done:
		return nil
	}
}

type networkError struct{ msg string }

func (e *networkError) Error() string { return e.msg }
