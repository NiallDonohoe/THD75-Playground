//go:build darwin && cgo

#include "coreaudio.h"

#include <AudioToolbox/AudioToolbox.h>
#include <CoreAudio/CoreAudio.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

static AudioObjectPropertyAddress addr(AudioObjectPropertySelector sel, AudioObjectPropertyScope scope) {
	AudioObjectPropertyAddress a = {sel, scope, kAudioObjectPropertyElementMain};
	return a;
}

int d75_device_count(void) {
	AudioObjectPropertyAddress a = addr(kAudioHardwarePropertyDevices, kAudioObjectPropertyScopeGlobal);
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &a, 0, NULL, &size) != noErr) return -1;
	return (int)(size / sizeof(AudioDeviceID));
}

int d75_device_ids(uint32_t *out, int max) {
	AudioObjectPropertyAddress a = addr(kAudioHardwarePropertyDevices, kAudioObjectPropertyScopeGlobal);
	UInt32 size = (UInt32)(max * sizeof(AudioDeviceID));
	if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &size, out) != noErr) return -1;
	return (int)(size / sizeof(AudioDeviceID));
}

uint32_t d75_default_input(void) {
	AudioObjectPropertyAddress a = addr(kAudioHardwarePropertyDefaultInputDevice, kAudioObjectPropertyScopeGlobal);
	AudioDeviceID dev = 0;
	UInt32 size = sizeof(dev);
	AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &size, &dev);
	return dev;
}

int d75_device_string(uint32_t dev, int which, char *buf, int len) {
	AudioObjectPropertySelector sel;
	switch (which) {
	case 0: sel = kAudioObjectPropertyName; break;
	case 1: sel = kAudioObjectPropertyManufacturer; break;
	case 2: sel = kAudioDevicePropertyDeviceUID; break;
	case 3: sel = kAudioDevicePropertyModelUID; break;
	default: return -1;
	}
	AudioObjectPropertyAddress a = addr(sel, kAudioObjectPropertyScopeGlobal);
	CFStringRef s = NULL;
	UInt32 size = sizeof(s);
	if (AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, &s) != noErr || s == NULL) return -1;
	Boolean ok = CFStringGetCString(s, buf, len, kCFStringEncodingUTF8);
	CFRelease(s);
	return ok ? (int)strlen(buf) : -1;
}

uint32_t d75_transport(uint32_t dev) {
	AudioObjectPropertyAddress a = addr(kAudioDevicePropertyTransportType, kAudioObjectPropertyScopeGlobal);
	UInt32 t = 0, size = sizeof(t);
	AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, &t);
	return t;
}

double d75_nominal_rate(uint32_t dev) {
	AudioObjectPropertyAddress a = addr(kAudioDevicePropertyNominalSampleRate, kAudioObjectPropertyScopeGlobal);
	Float64 r = 0;
	UInt32 size = sizeof(r);
	AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, &r);
	return r;
}

int d75_channels(uint32_t dev, int input) {
	AudioObjectPropertyAddress a = addr(kAudioDevicePropertyStreamConfiguration,
		input ? kAudioObjectPropertyScopeInput : kAudioObjectPropertyScopeOutput);
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(dev, &a, 0, NULL, &size) != noErr || size == 0) return 0;
	AudioBufferList *list = malloc(size);
	int n = 0;
	if (AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, list) == noErr) {
		for (UInt32 i = 0; i < list->mNumberBuffers; i++) n += (int)list->mBuffers[i].mNumberChannels;
	}
	free(list);
	return n;
}

int d75_available_rates(uint32_t dev, double *mins, double *maxs, int max) {
	AudioObjectPropertyAddress a = addr(kAudioDevicePropertyAvailableNominalSampleRates, kAudioObjectPropertyScopeGlobal);
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(dev, &a, 0, NULL, &size) != noErr || size == 0) return 0;
	AudioValueRange *r = malloc(size);
	int n = 0;
	if (AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, r) == noErr) {
		n = (int)(size / sizeof(AudioValueRange));
		if (n > max) n = max;
		for (int i = 0; i < n; i++) { mins[i] = r[i].mMinimum; maxs[i] = r[i].mMaximum; }
	}
	free(r);
	return n;
}

int d75_input_formats(uint32_t dev, d75_format *out, int max) {
	AudioObjectPropertyAddress a = addr(kAudioDevicePropertyStreams, kAudioObjectPropertyScopeInput);
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(dev, &a, 0, NULL, &size) != noErr || size == 0) return 0;
	AudioStreamID *streams = malloc(size);
	int n = 0;
	if (AudioObjectGetPropertyData(dev, &a, 0, NULL, &size, streams) == noErr) {
		int count = (int)(size / sizeof(AudioStreamID));
		for (int i = 0; i < count && n < max; i++) {
			AudioObjectPropertyAddress fa = addr(kAudioStreamPropertyPhysicalFormat, kAudioObjectPropertyScopeGlobal);
			AudioStreamBasicDescription d;
			UInt32 dsize = sizeof(d);
			if (AudioObjectGetPropertyData(streams[i], &fa, 0, NULL, &dsize, &d) != noErr) continue;
			out[n].sample_rate = d.mSampleRate;
			out[n].channels = d.mChannelsPerFrame;
			out[n].bits = d.mBitsPerChannel;
			out[n].is_float = (d.mFormatFlags & kAudioFormatFlagIsFloat) != 0;
			out[n].format_id = d.mFormatID;
			n++;
		}
	}
	free(streams);
	return n;
}

// --- capture ---

#define D75_BUFFERS 4

struct d75_capture {
	AudioQueueRef queue;
	AudioQueueBufferRef bufs[D75_BUFFERS];
	float *ring;
	size_t mask; // capacity in samples minus one
	_Atomic size_t w, r;
	_Atomic uint64_t overruns, callbacks;
	_Atomic int running;
};

// Runs on Core Audio's thread: copy into the ring and re-queue. No
// allocation, no locks, no Go.
static void on_input(void *user, AudioQueueRef q, AudioQueueBufferRef buf,
		const AudioTimeStamp *ts, UInt32 npackets, const AudioStreamPacketDescription *desc) {
	(void)ts; (void)npackets; (void)desc;
	d75_capture *c = user;
	atomic_fetch_add_explicit(&c->callbacks, 1, memory_order_relaxed);
	size_t n = buf->mAudioDataByteSize / sizeof(float);
	const float *src = buf->mAudioData;
	size_t w = atomic_load_explicit(&c->w, memory_order_relaxed);
	size_t r = atomic_load_explicit(&c->r, memory_order_acquire);
	size_t cap = c->mask + 1;
	if (cap - (w - r) < n) {
		atomic_fetch_add_explicit(&c->overruns, 1, memory_order_relaxed);
	} else {
		for (size_t i = 0; i < n; i++) c->ring[(w + i) & c->mask] = src[i];
		atomic_store_explicit(&c->w, w + n, memory_order_release);
	}
	if (atomic_load_explicit(&c->running, memory_order_relaxed)) AudioQueueEnqueueBuffer(q, buf, 0, NULL);
}

d75_capture *d75_capture_start(const char *uid, double rate, uint32_t channels, uint32_t ring_frames, int32_t *err) {
	d75_capture *c = calloc(1, sizeof(*c));
	size_t cap = 1;
	while (cap < (size_t)ring_frames * channels) cap <<= 1;
	c->ring = calloc(cap, sizeof(float));
	c->mask = cap - 1;
	atomic_store(&c->running, 1);

	AudioStreamBasicDescription fmt = {0};
	fmt.mSampleRate = rate;
	fmt.mFormatID = kAudioFormatLinearPCM;
	fmt.mFormatFlags = kLinearPCMFormatFlagIsFloat | kLinearPCMFormatFlagIsPacked;
	fmt.mChannelsPerFrame = channels;
	fmt.mBitsPerChannel = 32;
	fmt.mBytesPerFrame = 4 * channels;
	fmt.mFramesPerPacket = 1;
	fmt.mBytesPerPacket = fmt.mBytesPerFrame;

	OSStatus st = AudioQueueNewInput(&fmt, on_input, c, NULL, NULL, 0, &c->queue);
	if (st != noErr) goto fail;
	CFStringRef cfuid = CFStringCreateWithCString(NULL, uid, kCFStringEncodingUTF8);
	st = AudioQueueSetProperty(c->queue, kAudioQueueProperty_CurrentDevice, &cfuid, sizeof(cfuid));
	CFRelease(cfuid);
	if (st != noErr) goto fail;
	// 10 ms buffers.
	UInt32 bytes = (UInt32)(rate / 100) * fmt.mBytesPerFrame;
	for (int i = 0; i < D75_BUFFERS; i++) {
		st = AudioQueueAllocateBuffer(c->queue, bytes, &c->bufs[i]);
		if (st != noErr) goto fail;
		AudioQueueEnqueueBuffer(c->queue, c->bufs[i], 0, NULL);
	}
	st = AudioQueueStart(c->queue, NULL);
	if (st != noErr) goto fail;
	*err = 0;
	return c;
fail:
	*err = st;
	if (c->queue) AudioQueueDispose(c->queue, true);
	free(c->ring);
	free(c);
	return NULL;
}

size_t d75_capture_available(d75_capture *c) {
	return atomic_load_explicit(&c->w, memory_order_acquire) - atomic_load_explicit(&c->r, memory_order_relaxed);
}

size_t d75_capture_read(d75_capture *c, float *out, size_t max) {
	size_t r = atomic_load_explicit(&c->r, memory_order_relaxed);
	size_t avail = atomic_load_explicit(&c->w, memory_order_acquire) - r;
	size_t n = avail < max ? avail : max;
	for (size_t i = 0; i < n; i++) out[i] = c->ring[(r + i) & c->mask];
	atomic_store_explicit(&c->r, r + n, memory_order_release);
	return n;
}

void d75_capture_flush(d75_capture *c) {
	atomic_store_explicit(&c->r, atomic_load_explicit(&c->w, memory_order_acquire), memory_order_release);
}

uint64_t d75_capture_overruns(d75_capture *c) { return atomic_load(&c->overruns); }
uint64_t d75_capture_callbacks(d75_capture *c) { return atomic_load(&c->callbacks); }

void d75_capture_stop(d75_capture *c) {
	atomic_store(&c->running, 0);
	AudioQueueStop(c->queue, true);
	AudioQueueDispose(c->queue, true);
	free(c->ring);
	free(c);
}
