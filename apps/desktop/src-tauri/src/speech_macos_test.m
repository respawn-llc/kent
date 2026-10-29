#import "speech_macos.m"
#import <AudioToolbox/AudioToolbox.h>

@interface TestAudioOutput : AVCaptureAudioDataOutput
@end
@implementation TestAudioOutput
- (NSDictionary *)recommendedAudioSettingsForAssetWriterWithOutputFileType:(AVFileType)type {
    return @{ AVFormatIDKey: @(kAudioFormatMPEG4AAC), AVSampleRateKey: @48000,
              AVNumberOfChannelsKey: @1, AVEncoderBitRateKey: @64000 };
}
@end

typedef struct {
    FILE *file;
    dispatch_semaphore_t done;
    BOOL failed;
} TestCapture;

static void testReady(void *context) {}
static void testMeter(void *context, double power) {}
static void testSegment(void *context, const uint8_t *bytes, size_t count) {
    TestCapture *capture = context;
    assert(fwrite(bytes, 1, count, capture->file) == count);
}
static void testFinished(void *context, const char *error) {
    TestCapture *capture = context;
    if (error) {
        fprintf(stderr, "%s\n", error);
        capture->failed = YES;
    }
    dispatch_semaphore_signal(capture->done);
}

int main(int argc, const char **argv) {
    @autoreleasepool {
        assert(argc == 2);
        TestCapture capture = { .file = fopen(argv[1], "wb"),
            .done = dispatch_semaphore_create(0), .failed = NO };
        assert(capture.file);
        KentMicrophone *microphone = [KentMicrophone new];
        microphone.context = &capture;
        microphone.callbacks = (KentSpeechCallbacks){testReady, testMeter, testSegment, testFinished};
        microphone.queue = dispatch_queue_create("speech-test", DISPATCH_QUEUE_SERIAL);
        microphone.output = [TestAudioOutput new];
        microphone.lastMeterTime = kCMTimeInvalid;
        AVCaptureConnection *connection = [AVCaptureConnection connectionWithInputPorts:@[] output:microphone.output];
        AudioStreamBasicDescription format = { .mSampleRate = 48000,
            .mFormatID = kAudioFormatLinearPCM,
            .mFormatFlags = kAudioFormatFlagIsSignedInteger | kAudioFormatFlagIsPacked,
            .mBytesPerPacket = 2, .mFramesPerPacket = 1, .mBytesPerFrame = 2,
            .mChannelsPerFrame = 1, .mBitsPerChannel = 16 };
        CMAudioFormatDescriptionRef description;
        assert(CMAudioFormatDescriptionCreate(NULL, &format, 0, NULL, 0, NULL, NULL, &description) == noErr);
        for (int index = 0; index < 100; index++) {
            CMBlockBufferRef block;
            assert(CMBlockBufferCreateWithMemoryBlock(NULL, NULL, 960, NULL, NULL, 0, 960, 0, &block) == noErr);
            int16_t samples[480];
            for (int i = 0; i < 480; i++) samples[i] = (int16_t)(4000 * sin(2 * M_PI * 440 * (index * 480 + i) / 48000));
            assert(CMBlockBufferReplaceDataBytes(samples, block, 0, sizeof(samples)) == noErr);
            CMSampleTimingInfo timing = { .duration = CMTimeMake(1, 48000),
                .presentationTimeStamp = CMTimeMake(1655800LL * 48000 + index * 480, 48000),
                .decodeTimeStamp = kCMTimeInvalid };
            size_t size = 2;
            CMSampleBufferRef sample;
            assert(CMSampleBufferCreate(NULL, block, true, NULL, NULL, description, 480, 1, &timing, 1, &size, &sample) == noErr);
            dispatch_sync(microphone.queue, ^{
                [microphone captureOutput:microphone.output didOutputSampleBuffer:sample fromConnection:connection];
            });
            CFRelease(sample);
            CFRelease(block);
            [NSThread sleepForTimeInterval:0.01];
        }
        CFRelease(description);
        dispatch_async(microphone.queue, ^{ [microphone stopWithError:nil]; });
        assert(dispatch_semaphore_wait(capture.done, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC)) == 0);
        fclose(capture.file);
        assert(!capture.failed);
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:[NSURL fileURLWithPath:@(argv[1])] options:nil];
        double duration = CMTimeGetSeconds(asset.duration);
        fprintf(stderr, "Encoded duration: %.3f seconds\n", duration);
        assert(duration > 0.9 && duration < 1.2);
    }
    return 0;
}
