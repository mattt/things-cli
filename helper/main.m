#import <AppKit/AppKit.h>

// AppKit supplies URL events; all validation and IPC stay in the Go executable.
@interface CallbackDelegate : NSObject <NSApplicationDelegate>
@end

@implementation CallbackDelegate

- (void)application:(NSApplication *)application openURLs:(NSArray<NSURL *> *)urls {
    NSString *binary = [[[NSBundle mainBundle] executablePath] stringByDeletingLastPathComponent];
    binary = [binary stringByAppendingPathComponent:@"things"];
    for (NSURL *url in urls) {
        if (![url.scheme isEqualToString:@"things-cli-callback"]) {
            continue;
        }
        NSTask *task = [[NSTask alloc] init];
        task.executableURL = [NSURL fileURLWithPath:binary];
        task.arguments = @[@"callback", @"receive", url.absoluteString];
        task.standardOutput = [NSFileHandle fileHandleWithNullDevice];
        task.standardError = [NSFileHandle fileHandleWithNullDevice];
        NSError *error = nil;
        if ([task launchAndReturnError:&error]) {
            [task waitUntilExit];
        }
    }
    [application terminate:nil];
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    // Exit if opened without a URL; the helper is never a permanent service.
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
        [NSApp terminate:nil];
    });
}

@end

int main(int argc, const char *argv[]) {
    @autoreleasepool {
        if (argc == 2 && strcmp(argv[1], "--check-registration") == 0) {
            NSURL *url = [NSURL URLWithString:@"things-cli-callback:///probe"];
            NSURL *applicationURL = [[NSWorkspace sharedWorkspace] URLForApplicationToOpenURL:url];
            if (applicationURL == nil) {
                return 3;
            }
            NSString *identifier = [[NSBundle bundleWithURL:applicationURL] bundleIdentifier];
            if (identifier == nil) {
                return 3;
            }
            puts(identifier.UTF8String);
            return 0;
        }
        NSApplication *application = [NSApplication sharedApplication];
        CallbackDelegate *delegate = [[CallbackDelegate alloc] init];
        application.delegate = delegate;
        [application setActivationPolicy:NSApplicationActivationPolicyProhibited];
        [application run];
    }
    return 0;
}
