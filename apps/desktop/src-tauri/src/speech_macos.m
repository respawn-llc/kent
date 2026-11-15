#import "speech_macos.h"
#import <Accelerate/Accelerate.h>
#import <AVFoundation/AVFoundation.h>
#import <AudioToolbox/AudioToolbox.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>

enum {
    KentSpectrumSampleCount = 256,
    KentSpectrumBinCount = 56,
};

@interface KentMicrophone : NSObject <AVCaptureAudioDataOutputSampleBufferDelegate, AVAssetWriterDelegate>
{
    vDSP_DFT_Setup _spectrumSetup;
    float _sampleWindow[KentSpectrumSampleCount];
    float _windowedSamples[KentSpectrumSampleCount];
    float _inputReal[KentSpectrumSampleCount / 2];
    float _inputImaginary[KentSpectrumSampleCount / 2];
    float _outputReal[KentSpectrumSampleCount / 2];
    float _outputImaginary[KentSpectrumSampleCount / 2];
    float _window[KentSpectrumSampleCount];
    float _windowSum;
    NSUInteger _sampleCount;
    uint8_t _frequencyBins[KentSpectrumBinCount];
}
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
@property(nonatomic) CMTime sourceStartTime;
@end

@implementation KentMicrophone
- (void)dealloc {
    if (_spectrumSetup) vDSP_DFT_DestroySetup(_spectrumSetup);
}

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
    self.output.audioSettings = @{
        AVFormatIDKey: @(kAudioFormatLinearPCM),
        AVLinearPCMBitDepthKey: @32,
        AVLinearPCMIsFloatKey: @YES,
        AVLinearPCMIsBigEndianKey: @NO,
        AVLinearPCMIsNonInterleavedKey: @NO,
    };
    _spectrumSetup = vDSP_DFT_zrop_CreateSetup(NULL, KentSpectrumSampleCount, vDSP_DFT_FORWARD);
    if (!_spectrumSetup) {
        [self stopWithError:@"Microphone spectrum analysis is unavailable"];
        return;
    }
    vDSP_hann_window(_window, KentSpectrumSampleCount, vDSP_HANN_NORM);
    for (NSUInteger index = 0; index < KentSpectrumSampleCount; index++) {
        _windowSum += _window[index];
    }
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

- (void)analyzeSpectrumWindow {
    vDSP_vmul(_sampleWindow, 1, _window, 1, _windowedSamples, 1, KentSpectrumSampleCount);
    for (NSUInteger index = 0; index < KentSpectrumSampleCount / 2; index++) {
        _inputReal[index] = _windowedSamples[index * 2];
        _inputImaginary[index] = _windowedSamples[index * 2 + 1];
    }
    vDSP_DFT_Execute(
        _spectrumSetup, _inputReal, _inputImaginary, _outputReal, _outputImaginary);
    for (NSUInteger index = 0; index < KentSpectrumBinCount; index++) {
        NSUInteger frequency = index + 1;
        float magnitude = hypotf(_outputReal[frequency], _outputImaginary[frequency]) / _windowSum;
        float decibels = 20.0f * log10f(MAX(magnitude, 0.00025118864f));
        float scaled = (decibels + 72.0f) * (255.0f / 54.0f);
        _frequencyBins[index] = (uint8_t)lrintf(MIN(255.0f, MAX(0.0f, scaled)));
    }
    _sampleCount = 0;
}

- (BOOL)updateSpectrumFromSampleBuffer:(CMSampleBufferRef)sample {
    CMFormatDescriptionRef description = CMSampleBufferGetFormatDescription(sample);
    const AudioStreamBasicDescription *format = description
        ? CMAudioFormatDescriptionGetStreamBasicDescription((CMAudioFormatDescriptionRef)description)
        : NULL;
    if (!format || format->mFormatID != kAudioFormatLinearPCM ||
        (format->mFormatFlags & kAudioFormatFlagIsFloat) == 0 ||
        (format->mFormatFlags & kAudioFormatFlagIsNonInterleaved) != 0 ||
        format->mBitsPerChannel != 32 || format->mChannelsPerFrame == 0 ||
        format->mBytesPerFrame != format->mChannelsPerFrame * sizeof(float)) {
        [self stopWithError:@"Microphone sample format is not Float32 linear PCM"];
        return NO;
    }

    AudioBufferList audioBufferList = {0};
    CMBlockBufferRef retainedBuffer = NULL;
    OSStatus status = CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer(
        sample, NULL, &audioBufferList, sizeof(audioBufferList),
        kCFAllocatorDefault, kCFAllocatorDefault,
        kCMSampleBufferFlag_AudioBufferList_Assure16ByteAlignment, &retainedBuffer);
    if (status != noErr) {
        if (retainedBuffer) CFRelease(retainedBuffer);
        [self stopWithError:[NSString stringWithFormat:@"Microphone audio samples could not be read (%d)", (int)status]];
        return NO;
    }

    size_t channelCount = format->mChannelsPerFrame;
    size_t sampleCount = (size_t)CMSampleBufferGetNumSamples(sample);
    AudioBuffer *buffer = &audioBufferList.mBuffers[0];
    if (audioBufferList.mNumberBuffers != 1 || buffer->mNumberChannels != channelCount ||
        buffer->mData == NULL || buffer->mDataByteSize / format->mBytesPerFrame < sampleCount) {
        CFRelease(retainedBuffer);
        [self stopWithError:@"Microphone audio sample layout is unavailable"];
        return NO;
    }

    const float *samples = buffer->mData;
    for (size_t frame = 0; frame < sampleCount; frame++) {
        float value = 0;
        for (size_t channel = 0; channel < channelCount; channel++) {
            value += samples[frame * channelCount + channel];
        }
        _sampleWindow[_sampleCount++] = value / channelCount;
        if (_sampleCount == KentSpectrumSampleCount) [self analyzeSpectrumWindow];
    }
    CFRelease(retainedBuffer);
    return YES;
}

- (void)captureOutput:(AVCaptureOutput *)output
 didOutputSampleBuffer:(CMSampleBufferRef)sample
       fromConnection:(AVCaptureConnection *)connection {
    if (self.stopping || !CMSampleBufferDataIsReady(sample) || CMSampleBufferGetNumSamples(sample) == 0) return;
    if (![self updateSpectrumFromSampleBuffer:sample]) return;
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
        // Fragmented MP4 consumers may ignore edit lists. Keep fragment timestamps
        // relative to the recording, not the capture device's host clock.
        self.writer.initialSegmentStartTime = kCMTimeZero;
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
        self.sourceStartTime = CMSampleBufferGetPresentationTimeStamp(sample);
        [self.writer startSessionAtSourceTime:kCMTimeZero];
        // Silence is a real buffer and establishes readiness too.
        self.callbacks.ready(self.context);
    }
    CMSampleTimingInfo timing;
    OSStatus status = CMSampleBufferGetSampleTimingInfo(sample, 0, &timing);
    CMSampleBufferRef relativeSample = NULL;
    if (status == noErr) {
        timing.presentationTimeStamp = CMTimeSubtract(timing.presentationTimeStamp, self.sourceStartTime);
        if (CMTIME_IS_VALID(timing.decodeTimeStamp)) {
            timing.decodeTimeStamp = CMTimeSubtract(timing.decodeTimeStamp, self.sourceStartTime);
        }
        status = CMSampleBufferCreateCopyWithNewTiming(kCFAllocatorDefault, sample, 1, &timing, &relativeSample);
    }
    if (status != noErr) {
        [self stopWithError:[NSString stringWithFormat:@"Microphone sample timing failed (%d)", (int)status]];
        return;
    }
    BOOL appended = self.input.readyForMoreMediaData && [self.input appendSampleBuffer:relativeSample];
    CFRelease(relativeSample);
    if (!appended) {
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
    self.callbacks.meter(self.context, power, _frequencyBins, KentSpectrumBinCount);
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
