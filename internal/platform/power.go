package platform

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <IOKit/IOMessage.h>
#include <CoreFoundation/CoreFoundation.h>

// Callback bridge
extern void goPowerCallback(int messageType);

static io_connect_t rootPort;
static IONotificationPortRef notifyPort;
static io_object_t notifierObject;

static void powerCallbackC(void *refCon, io_service_t service, natural_t messageType, void *messageArgument) {
    if (messageType == kIOMessageCanSystemSleep || messageType == kIOMessageSystemWillSleep) {
        IOAllowPowerChange(rootPort, (long)messageArgument);
        goPowerCallback(1); // sleep
    } else if (messageType == kIOMessageSystemHasPoweredOn) {
        goPowerCallback(2); // wake
    }
}

static int setupPowerNotifications() {
    rootPort = IORegisterForSystemPower(NULL, &notifyPort, powerCallbackC, &notifierObject);
    if (rootPort == 0) {
        return -1;
    }
    CFRunLoopAddSource(CFRunLoopGetCurrent(),
        IONotificationPortGetRunLoopSource(notifyPort),
        kCFRunLoopDefaultMode);
    return 0;
}

static void runPowerLoop() {
    CFRunLoopRun();
}

static void stopPowerLoop() {
    CFRunLoopStop(CFRunLoopGetCurrent());
}
*/
import "C"

import (
	"context"
	"log/slog"
)

// PowerEvent represents a system power change.
type PowerEvent int

const (
	PowerSleep PowerEvent = 1
	PowerWake  PowerEvent = 2
)

func (e PowerEvent) String() string {
	switch e {
	case PowerSleep:
		return "sleep"
	case PowerWake:
		return "wake"
	default:
		return "unknown"
	}
}

var powerChan chan PowerEvent

//export goPowerCallback
func goPowerCallback(messageType C.int) {
	if powerChan != nil {
		powerChan <- PowerEvent(messageType)
	}
}

// PowerMonitor watches for macOS sleep/wake events.
type PowerMonitor struct {
	log    *slog.Logger
	Events chan PowerEvent
}

func NewPowerMonitor(log *slog.Logger) *PowerMonitor {
	ch := make(chan PowerEvent, 10)
	powerChan = ch
	return &PowerMonitor{
		log:    log.With("component", "power"),
		Events: ch,
	}
}

// Run starts the power event listener. Must be called from a dedicated goroutine.
func (p *PowerMonitor) Run(ctx context.Context) error {
	if C.setupPowerNotifications() != 0 {
		return errPowerSetup
	}

	p.log.Info("power monitor started")

	// Run CFRunLoop in a goroutine so we can select on ctx
	done := make(chan struct{})
	go func() {
		C.runPowerLoop()
		close(done)
	}()

	select {
	case <-ctx.Done():
		C.stopPowerLoop()
		<-done
		return ctx.Err()
	case <-done:
		return nil
	}
}

var errPowerSetup = &powerError{"failed to register for power notifications"}

type powerError struct{ msg string }

func (e *powerError) Error() string { return e.msg }
