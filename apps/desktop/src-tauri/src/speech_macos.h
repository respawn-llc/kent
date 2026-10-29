#pragma once
#include <stddef.h>
#include <stdint.h>

// Callbacks are serialized. The owner must keep context alive until finished.
typedef struct {
    void (*ready)(void *context);
    void (*meter)(void *context, double power);
    void (*segment)(void *context, const uint8_t *bytes, size_t count);
    void (*finished)(void *context, const char *error);
} KentSpeechCallbacks;

void *kent_speech_start(void *context, KentSpeechCallbacks callbacks);
void kent_speech_stop(void *capture);
