//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#import <dispatch/dispatch.h>

static void makeEditorAccessory(void) {
    // Wails registers every process as a regular application during launch.
    // Apply this after the WebView loads, on AppKit's main thread.
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *window = NSApp.keyWindow ?: NSApp.mainWindow;
        if (!window) window = NSApp.windows.firstObject;
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        // Changing policy can order the existing window out. Restore it so
        // keyboard focus and WebKit animation/layout callbacks keep working.
        [window makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
    });
}
*/
import "C"

func makeEditorAccessory() { C.makeEditorAccessory() }
