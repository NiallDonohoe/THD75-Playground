#ifndef D75_COREAUDIO_H
#define D75_COREAUDIO_H

#include <stdint.h>
#include <stddef.h>

typedef struct {
	double sample_rate;
	uint32_t channels;
	uint32_t bits;
	uint32_t is_float;
	uint32_t format_id;
} d75_format;

int d75_device_count(void);
int d75_device_ids(uint32_t *out, int max);
uint32_t d75_default_input(void);
// Copies a string property into buf (UTF-8). which: 0 name, 1 manufacturer,
// 2 UID, 3 model UID. Returns length or -1.
int d75_device_string(uint32_t dev, int which, char *buf, int len);
uint32_t d75_transport(uint32_t dev);
double d75_nominal_rate(uint32_t dev);
// input != 0 for the input scope.
int d75_channels(uint32_t dev, int input);
int d75_available_rates(uint32_t dev, double *mins, double *maxs, int max);
int d75_input_formats(uint32_t dev, d75_format *out, int max);

typedef struct d75_capture d75_capture;
// Starts capture of interleaved float32 frames from the device with this UID.
// ring_frames is the ring buffer capacity in frames (rounded up to a power
// of two). Returns NULL and sets *err (OSStatus) on failure.
d75_capture *d75_capture_start(const char *uid, double rate, uint32_t channels, uint32_t ring_frames, int32_t *err);
// Copies up to max samples out of the ring; returns the number copied.
size_t d75_capture_read(d75_capture *c, float *out, size_t max);
size_t d75_capture_available(d75_capture *c);
void d75_capture_flush(d75_capture *c);
uint64_t d75_capture_overruns(d75_capture *c);
uint64_t d75_capture_callbacks(d75_capture *c);
void d75_capture_stop(d75_capture *c);

#endif
