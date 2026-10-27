// Headless encoder verification. No device acquisition or microphone permission.
#import "../src/speech_macos.m"
#import <AudioToolbox/AudioToolbox.h>

@interface SyntheticAudioOutput : AVCaptureAudioDataOutput
@end
@implementation SyntheticAudioOutput
- (NSDictionary *)recommendedAudioSettingsForAssetWriterWithOutputFileType:(AVFileType)type {
    return @{
        AVFormatIDKey: @(kAudioFormatMPEG4AAC),
        AVSampleRateKey: @48000,
        AVNumberOfChannelsKey: @1,
    };
}
@end

typedef struct {
    __unsafe_unretained NSMutableData *data;
    dispatch_semaphore_t finished;
    dispatch_semaphore_t streamed;
    unsigned ready;
    unsigned segments;
    BOOL failed;
} TestCapture;

static void ready(void *context) { ((TestCapture *)context)->ready++; }
static void meter(void *context, double power) {}
static void segment(void *context, const uint8_t *bytes, size_t count) {
    TestCapture *capture = context;
    [capture->data appendBytes:bytes length:count];
    capture->segments++;
    dispatch_semaphore_signal(capture->streamed);
}
static void finished(void *context, const char *error) {
    TestCapture *capture = context;
    capture->failed = error != NULL;
    if (error) fprintf(stderr, "Encoding failed: %s\n", error);
    dispatch_semaphore_signal(capture->finished);
}

static CMSampleBufferRef silence(unsigned index) {
    AudioStreamBasicDescription format = {
        .mSampleRate = 48000,
        .mFormatID = kAudioFormatLinearPCM,
        .mFormatFlags = kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked,
        .mBytesPerPacket = sizeof(float),
        .mFramesPerPacket = 1,
        .mBytesPerFrame = sizeof(float),
        .mChannelsPerFrame = 1,
        .mBitsPerChannel = 32,
    };
    CMAudioFormatDescriptionRef description = NULL;
    assert(CMAudioFormatDescriptionCreate(kCFAllocatorDefault, &format, 0, NULL, 0, NULL,
        NULL, &description) == noErr);
    CMBlockBufferRef block = NULL;
    assert(CMBlockBufferCreateWithMemoryBlock(kCFAllocatorDefault, NULL, 960 * sizeof(float),
        kCFAllocatorDefault, NULL, 0, 960 * sizeof(float), 0, &block) == noErr);
    assert(CMBlockBufferFillDataBytes(0, block, 0, 960 * sizeof(float)) == noErr);
    CMSampleTimingInfo timing = {
        .duration = CMTimeMake(1, 48000),
        .presentationTimeStamp = CMTimeMake(index * 960, 48000),
        .decodeTimeStamp = kCMTimeInvalid,
    };
    size_t sampleSize = sizeof(float);
    CMSampleBufferRef sample = NULL;
    assert(CMSampleBufferCreateReady(kCFAllocatorDefault, block, description, 960,
        1, &timing, 1, &sampleSize, &sample) == noErr);
    CFRelease(block);
    CFRelease(description);
    return sample;
}

int main(void) {
    @autoreleasepool {
        NSMutableData *data = [NSMutableData data];
        TestCapture result = {
            .data = data,
            .finished = dispatch_semaphore_create(0),
            .streamed = dispatch_semaphore_create(0),
        };
        KentMicrophone *capture = [KentMicrophone new];
        capture.context = &result;
        capture.callbacks = (KentSpeechCallbacks){ready, meter, segment, finished};
        capture.queue = dispatch_queue_create("sh.kent.synthetic-audio", DISPATCH_QUEUE_SERIAL);
        capture.output = [SyntheticAudioOutput new];
        AVCaptureConnection *connection = [AVCaptureConnection connectionWithInputPorts:@[] output:capture.output];
        capture.lastMeterTime = kCMTimeInvalid;
        for (unsigned index = 0; index < 100; index++) {
            // Synthetic samples are produced faster than a physical microphone.
            __block BOOL readyForData = NO;
            do {
                dispatch_sync(capture.queue, ^{
                    assert(!result.failed);
                    readyForData = !capture.input || capture.input.readyForMoreMediaData;
                });
                if (readyForData) break;
                usleep(1000);
            } while (YES);
            CMSampleBufferRef sample = silence(index);
            dispatch_sync(capture.queue, ^{
                [capture captureOutput:capture.output didOutputSampleBuffer:sample fromConnection:connection];
            });
            CFRelease(sample);
        }
        for (unsigned segment = 0; segment < 3; segment++) {
            assert(dispatch_semaphore_wait(result.streamed, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC)) == 0);
        }
        dispatch_sync(capture.queue, ^{
            assert(result.ready == 1);
            assert(result.segments >= 3);
            [capture stopWithError:nil];
        });
        assert(dispatch_semaphore_wait(result.finished, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC)) == 0);
        assert(!result.failed);
        assert(result.ready == 1);
        // Initialization plus several media segments must arrive before Stop.
        assert(result.segments >= 3);
        NSURL *file = [NSURL fileURLWithPath:[NSTemporaryDirectory() stringByAppendingPathComponent:
            [NSUUID.UUID.UUIDString stringByAppendingPathExtension:@"mp4"]]];
        assert([data writeToURL:file options:NSDataWritingAtomic error:nil]);
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:file options:nil];
        NSError *error = nil;
        AVAssetReader *reader = [[AVAssetReader alloc] initWithAsset:asset error:&error];
        assert(reader != nil);
        dispatch_semaphore_t loaded = dispatch_semaphore_create(0);
        __block AVAssetTrack *track = nil;
        [asset loadTracksWithMediaType:AVMediaTypeAudio completionHandler:^(NSArray<AVAssetTrack *> *tracks, NSError *error) {
            assert(error == nil);
            track = tracks.firstObject;
            dispatch_semaphore_signal(loaded);
        }];
        assert(dispatch_semaphore_wait(loaded, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC)) == 0);
        assert(track != nil);
        AVAssetReaderTrackOutput *output = [AVAssetReaderTrackOutput assetReaderTrackOutputWithTrack:track
            outputSettings:@{AVFormatIDKey: @(kAudioFormatLinearPCM)}];
        [reader addOutput:output];
        assert([reader startReading]);
        size_t frames = 0;
        CMSampleBufferRef sample;
        while ((sample = [output copyNextSampleBuffer])) {
            frames += CMSampleBufferGetNumSamples(sample);
            CFRelease(sample);
        }
        assert(reader.status == AVAssetReaderStatusCompleted);
        assert(frames >= 96000);
        assert([NSFileManager.defaultManager removeItemAtURL:file error:nil]);
    }
    return 0;
}
