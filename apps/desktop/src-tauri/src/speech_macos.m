#import "speech_macos.h"
#import <AVFoundation/AVFoundation.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>

@interface KentMicrophone : NSObject <AVCaptureAudioDataOutputSampleBufferDelegate, AVAssetWriterDelegate>
@property(nonatomic) void *context;
@property(nonatomic) KentSpeechCallbacks callbacks;
@property(nonatomic) dispatch_queue_t queue;
@property(nonatomic) AVCaptureSession *session;
@property(nonatomic) AVCaptureAudioDataOutput *output;
@property(nonatomic) AVAssetWriter *writer;
@property(nonatomic) AVAssetWriterInput *input;
@property(nonatomic) id runtimeErrorObserver;
@property(nonatomic) BOOL stopping;
@property(nonatomic) BOOL completed;
@property(nonatomic) CMTime lastMeterTime;
@end

@implementation KentMicrophone
- (void)complete:(NSString *)error {
    if (self.completed) return;
    self.completed = YES;
    if (self.runtimeErrorObserver) {
        [NSNotificationCenter.defaultCenter removeObserver:self.runtimeErrorObserver];
        self.runtimeErrorObserver = nil;
    }
    self.callbacks.finished(self.context, error ? error.UTF8String : NULL);
}

- (void)stopWithError:(NSString *)error {
    if (self.stopping) return;
    self.stopping = YES;
    [self.session stopRunning];
    [self.output setSampleBufferDelegate:nil queue:NULL];
    if (!self.writer || self.writer.status != AVAssetWriterStatusWriting) {
        [self complete:error ?: self.writer.error.localizedDescription];
        return;
    }
    [self.input markAsFinished];
    [self.writer finishWritingWithCompletionHandler:^{
        dispatch_async(self.queue, ^{
            [self complete:error ?: self.writer.error.localizedDescription];
        });
    }];
}

- (void)prepare {
    if (self.stopping) return;
    NSError *error = nil;
    AVCaptureDevice *device = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeAudio];
    if (!device) {
        [self stopWithError:@"No microphone is available"];
        return;
    }
    AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:device error:&error];
    if (!input) {
        [self stopWithError:error.localizedDescription];
        return;
    }
    self.session = [AVCaptureSession new];
    self.output = [AVCaptureAudioDataOutput new];
    [self.session beginConfiguration];
    if (![self.session canAddInput:input] || ![self.session canAddOutput:self.output]) {
        [self.session commitConfiguration];
        [self stopWithError:@"Microphone capture configuration is unavailable"];
        return;
    }
    [self.session addInput:input];
    [self.session addOutput:self.output];
    [self.output setSampleBufferDelegate:self queue:self.queue];
    [self.session commitConfiguration];
    __weak KentMicrophone *weakSelf = self;
    self.runtimeErrorObserver = [NSNotificationCenter.defaultCenter
        addObserverForName:AVCaptureSessionRuntimeErrorNotification object:self.session queue:nil
        usingBlock:^(NSNotification *notification) {
            NSError *failure = notification.userInfo[AVCaptureSessionErrorKey];
            KentMicrophone *capture = weakSelf;
            if (capture) dispatch_async(capture.queue, ^{
                [capture stopWithError:failure.localizedDescription ?: @"Microphone capture failed"];
            });
        }];
    [self.session startRunning];
}

- (void)captureOutput:(AVCaptureOutput *)output
 didOutputSampleBuffer:(CMSampleBufferRef)sample
       fromConnection:(AVCaptureConnection *)connection {
    if (self.stopping || !CMSampleBufferDataIsReady(sample) || CMSampleBufferGetNumSamples(sample) == 0) return;
    if (!self.writer) {
        // Capture's recommended settings include Apple's encoder choices rather
        // than a Kent-selected speech bitrate.
        NSDictionary *settings = [self.output recommendedAudioSettingsForAssetWriterWithOutputFileType:AVFileTypeMPEG4];
        if (!settings) {
            [self stopWithError:@"Recommended microphone encoding is unavailable"];
            return;
        }
        self.writer = [[AVAssetWriter alloc] initWithContentType:UTTypeMPEG4Movie];
        self.writer.outputFileTypeProfile = AVFileTypeProfileMPEG4AppleHLS;
        self.writer.preferredOutputSegmentInterval = CMTimeMake(1, 4);
        self.writer.initialSegmentStartTime = CMSampleBufferGetPresentationTimeStamp(sample);
        self.writer.delegate = self;
        self.input = [AVAssetWriterInput assetWriterInputWithMediaType:AVMediaTypeAudio
            outputSettings:settings sourceFormatHint:CMSampleBufferGetFormatDescription(sample)];
        self.input.expectsMediaDataInRealTime = YES;
        if (![self.writer canAddInput:self.input]) {
            [self stopWithError:@"Microphone encoder cannot accept audio"];
            return;
        }
        [self.writer addInput:self.input];
        if (![self.writer startWriting]) {
            [self stopWithError:self.writer.error.localizedDescription];
            return;
        }
        [self.writer startSessionAtSourceTime:CMSampleBufferGetPresentationTimeStamp(sample)];
        // Silence is a real buffer and establishes readiness too.
        self.callbacks.ready(self.context);
    }
    if (!self.input.readyForMoreMediaData || ![self.input appendSampleBuffer:sample]) {
        [self stopWithError:self.writer.error.localizedDescription ?: @"Microphone encoder could not keep up"];
        return;
    }
    CMTime now = CMSampleBufferGetPresentationTimeStamp(sample);
    if (CMTIME_IS_VALID(self.lastMeterTime) &&
        CMTimeGetSeconds(CMTimeSubtract(now, self.lastMeterTime)) < 0.05) return;
    self.lastMeterTime = now;
    double power = 0;
    for (AVCaptureAudioChannel *channel in connection.audioChannels) {
        power = MAX(power, pow(10.0, channel.averagePowerLevel / 20.0));
    }
    self.callbacks.meter(self.context, power);
}

- (void)assetWriter:(AVAssetWriter *)writer didOutputSegmentData:(NSData *)data
       segmentType:(AVAssetSegmentType)type {
    // AVAssetWriter may call its delegate off the sample queue. Copy and order
    // delivery there so the final completion follows every compressed segment.
    dispatch_async(self.queue, ^{
        self.callbacks.segment(self.context, data.bytes, data.length);
    });
}
@end

void *kent_speech_start(void *context, KentSpeechCallbacks callbacks) {
    KentMicrophone *capture = [KentMicrophone new];
    capture.context = context;
    capture.callbacks = callbacks;
    capture.lastMeterTime = kCMTimeInvalid;
    capture.queue = dispatch_queue_create("sh.kent.microphone", DISPATCH_QUEUE_SERIAL);
    void *retained = (__bridge_retained void *)capture;
    [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {
        dispatch_async(capture.queue, ^{
            if (granted) [capture prepare];
            else [capture stopWithError:@"Microphone access was denied"];
        });
    }];
    return retained;
}

void kent_speech_stop(void *pointer) {
    KentMicrophone *capture = (__bridge_transfer KentMicrophone *)pointer;
    dispatch_async(capture.queue, ^{
        [capture stopWithError:nil];
    });
}
