//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#import <dispatch/dispatch.h>

static void presentEditorWindow(void) {
    // Wails registers every process as a regular application during launch.
    // Keep the window hidden until its content is ready and set the policy
    // before ordering it front for the first time.
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow *window = NSApp.keyWindow ?: NSApp.mainWindow;
        if (!window) window = NSApp.windows.firstObject;
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        [window makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
    });
}
*/
import "C"

func presentEditorWindow() { C.presentEditorWindow() }
