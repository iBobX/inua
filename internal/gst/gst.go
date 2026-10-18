// Package gst plays audio and video with GStreamer, loaded at run time
// through purego as GTK is: no cgo and nothing to build against. The
// players read GStreamer's bus in a goroutine, so no C callbacks run Go
// code (docs/feat/players.md).
package gst

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"codeberg.org/puregotk/purego"
)

// The C functions, by library.
var (
	gstInit              func(argc, argv uintptr)
	elementFactoryMake   func(factory, name string) uintptr
	parseLaunch          func(desc string, err *uintptr) uintptr
	parseBinFromDesc     func(desc string, ghost bool, err *uintptr) uintptr
	binGetByName         func(bin uintptr, name string) uintptr
	elementSetState      func(el uintptr, state int32) int32
	elementGetState      func(el uintptr, state, pending *int32, timeout uint64) int32
	elementQueryPosition func(el uintptr, format int32, cur *int64) bool
	elementQueryDuration func(el uintptr, format int32, cur *int64) bool
	elementSeek          func(el uintptr, rate float64, format, flags, startType int32, start int64, stopType int32, stop int64) bool
	elementGetBus        func(el uintptr) uintptr
	busTimedPopFiltered  func(bus uintptr, timeout uint64, types uint32) uintptr
	messageGetStructure  func(msg uintptr) uintptr
	messageParseError    func(msg uintptr, gerr, debug *uintptr)
	messageParseTag      func(msg uintptr, list *uintptr)
	messageTypeGetName   func(t uint32) string
	structureToString    func(s uintptr) uintptr
	structureGetName     func(s uintptr) string
	structureGetInt      func(s uintptr, field string, v *int32) bool
	tagListGetString     func(list uintptr, tag string, v *uintptr) bool
	tagListGetUint       func(list uintptr, tag string, v *uint32) bool
	miniObjectUnref      func(obj uintptr)
	objectUnref          func(obj uintptr)
	utilSetObjectArg     func(obj uintptr, name, value string)
	sampleGetBuffer      func(sample uintptr) uintptr
	sampleGetCaps        func(sample uintptr) uintptr
	capsGetStructure     func(caps uintptr, i uint32) uintptr
	bufferMap            func(buf uintptr, info *mapInfo, flags int32) bool
	bufferUnmap          func(buf uintptr, info *mapInfo)

	appSinkTryPullSample func(sink uintptr, timeout uint64) uintptr
	appSrcPushBuffer     func(src, buf uintptr) int32
	bufferNewAllocate    func(allocator, size, params uintptr) uintptr
	bufferFill           func(buf, offset uintptr, src unsafe.Pointer, size uintptr) uintptr
	elementFactoryFind   func(name string) uintptr

	discovererNew             func(timeout uint64, err *uintptr) uintptr
	discovererDiscoverURI     func(d uintptr, uri string, err *uintptr) uintptr
	discovererInfoGetDuration func(info uintptr) uint64
	discovererInfoGetTags     func(info uintptr) uintptr
	discovererInfoGetAudio    func(info uintptr) uintptr
	discovererInfoGetResult   func(info uintptr) int32
	audioInfoGetSampleRate    func(ai uintptr) uint32
	audioInfoGetChannels      func(ai uintptr) uint32
	audioInfoGetBitrate       func(ai uintptr) uint32
	streamInfoListFree        func(list uintptr)

	gObjectSetPtr func(obj uintptr, name string, v uintptr, end uintptr)
	gObjectSetInt func(obj uintptr, name string, v int32, end uintptr)
	gObjectGetInt func(obj uintptr, name string, v *int32, end uintptr)
	gObjectUnref  func(obj uintptr)
	gFree         func(p uintptr)
	gErrorFree    func(e uintptr)
)

// mapInfo is GstMapInfo.
type mapInfo struct {
	memory   uintptr
	flags    int32
	_        int32
	data     uintptr
	size     uintptr
	maxsize  uintptr
	user     [4]uintptr
	reserved [4]uintptr
}

// GStreamer's values used here (gstelement.h, gstformat.h, gstmessage.h,
// gstevent.h).
const (
	stateNull    = 1
	stateReady   = 2
	statePaused  = 3
	statePlaying = 4

	formatTime = 3

	seekFlagFlush    = 1 << 0
	seekFlagAccurate = 1 << 1
	seekFlagKeyUnit  = 1 << 2
	seekTypeSet      = 1
	seekTypeNone     = 0

	msgEOS             = 1 << 0
	msgError           = 1 << 1
	msgTag             = 1 << 4
	msgStateChanged    = 1 << 6
	msgElement         = 1 << 15
	msgDurationChanged = 1 << 18
	msgAsyncDone       = 1 << 21
	msgStreamStart     = 1 << 28

	mapRead = 1
)

var (
	loadOnce sync.Once
	loadErr  error
)

// Init loads GStreamer and starts it, once; an error says what is
// missing.
func Init() error {
	loadOnce.Do(func() { loadErr = load() })
	return loadErr
}

func open(name string) (uintptr, error) {
	h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return h, nil
}

func load() error {
	core, err := open("libgstreamer-1.0.so.0")
	if err != nil {
		return ErrMissing
	}
	app, err1 := open("libgstapp-1.0.so.0")
	pb, err2 := open("libgstpbutils-1.0.so.0")
	gobj, err3 := open("libgobject-2.0.so.0")
	glib, err4 := open("libglib-2.0.so.0")
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return fmt.Errorf("%w (%v)", ErrMissing, err)
	}
	for name, f := range map[string]any{
		"gst_init": &gstInit, "gst_element_factory_make": &elementFactoryMake,
		"gst_parse_launch": &parseLaunch, "gst_parse_bin_from_description": &parseBinFromDesc,
		"gst_bin_get_by_name": &binGetByName, "gst_element_set_state": &elementSetState,
		"gst_element_get_state":      &elementGetState,
		"gst_element_query_position": &elementQueryPosition, "gst_element_query_duration": &elementQueryDuration,
		"gst_element_seek": &elementSeek, "gst_element_get_bus": &elementGetBus,
		"gst_bus_timed_pop_filtered": &busTimedPopFiltered, "gst_message_get_structure": &messageGetStructure,
		"gst_message_parse_error": &messageParseError, "gst_message_parse_tag": &messageParseTag,
		"gst_message_type_get_name": &messageTypeGetName, "gst_structure_to_string": &structureToString,
		"gst_structure_get_name": &structureGetName, "gst_structure_get_int": &structureGetInt,
		"gst_tag_list_get_string": &tagListGetString, "gst_tag_list_get_uint": &tagListGetUint,
		"gst_mini_object_unref": &miniObjectUnref, "gst_object_unref": &objectUnref,
		"gst_util_set_object_arg": &utilSetObjectArg, "gst_sample_get_buffer": &sampleGetBuffer,
		"gst_sample_get_caps": &sampleGetCaps, "gst_caps_get_structure": &capsGetStructure,
		"gst_buffer_map": &bufferMap, "gst_buffer_unmap": &bufferUnmap,
		"gst_buffer_new_allocate": &bufferNewAllocate, "gst_buffer_fill": &bufferFill,
		"gst_element_factory_find": &elementFactoryFind,
	} {
		purego.RegisterLibFunc(f, core, name)
	}
	purego.RegisterLibFunc(&appSinkTryPullSample, app, "gst_app_sink_try_pull_sample")
	purego.RegisterLibFunc(&appSrcPushBuffer, app, "gst_app_src_push_buffer")
	purego.RegisterLibFunc(&discovererNew, pb, "gst_discoverer_new")
	purego.RegisterLibFunc(&discovererDiscoverURI, pb, "gst_discoverer_discover_uri")
	purego.RegisterLibFunc(&discovererInfoGetDuration, pb, "gst_discoverer_info_get_duration")
	purego.RegisterLibFunc(&discovererInfoGetTags, pb, "gst_discoverer_info_get_tags")
	purego.RegisterLibFunc(&discovererInfoGetAudio, pb, "gst_discoverer_info_get_audio_streams")
	purego.RegisterLibFunc(&discovererInfoGetResult, pb, "gst_discoverer_info_get_result")
	purego.RegisterLibFunc(&audioInfoGetSampleRate, pb, "gst_discoverer_audio_info_get_sample_rate")
	purego.RegisterLibFunc(&audioInfoGetChannels, pb, "gst_discoverer_audio_info_get_channels")
	purego.RegisterLibFunc(&audioInfoGetBitrate, pb, "gst_discoverer_audio_info_get_bitrate")
	purego.RegisterLibFunc(&streamInfoListFree, pb, "gst_discoverer_stream_info_list_free")
	purego.RegisterLibFunc(&gObjectSetPtr, gobj, "g_object_set")
	purego.RegisterLibFunc(&gObjectSetInt, gobj, "g_object_set")
	purego.RegisterLibFunc(&gObjectGetInt, gobj, "g_object_get")
	purego.RegisterLibFunc(&gObjectUnref, gobj, "g_object_unref")
	purego.RegisterLibFunc(&gFree, glib, "g_free")
	purego.RegisterLibFunc(&gErrorFree, glib, "g_error_free")
	gstInit(0, 0)
	if messageTypeGetName(msgEOS) != "eos" || messageTypeGetName(msgElement) != "element" {
		return errors.New("gst: this GStreamer's message types are not the ones known here")
	}
	return nil
}

// ErrMissing is GStreamer not installed.
var ErrMissing = errors.New("GStreamer is not installed (gstreamer1.0-plugins-base, -good, -bad, -ugly and -libav, or your system's names for them)")

// cmem is C memory at address p, which Go's collector neither moves nor
// frees (go vet cannot tell it from a Go pointer kept as a number).
func cmem(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// cstring reads a C string; free g_frees it after.
func cstring(p uintptr, free bool) string {
	if p == 0 {
		return ""
	}
	var b []byte
	for i := uintptr(0); ; i++ {
		c := *(*byte)(cmem(p + i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	if free {
		gFree(p)
	}
	return string(b)
}

// gerror is a GError's message, freeing it.
func gerror(e uintptr) error {
	if e == 0 {
		return nil
	}
	// GError: GQuark domain (4), gint code (4), gchar *message.
	msg := cstring(*(*uintptr)(cmem(e + 8)), false)
	gErrorFree(e)
	return errors.New(msg)
}

// messageType reads GST_MESSAGE_TYPE: after the 64 bytes of its
// GstMiniObject.
func messageType(msg uintptr) uint32 { return *(*uint32)(cmem(msg + 64)) }

// Launch runs a pipeline described as gst-launch does, until it ends:
// tests make their media with it.
func Launch(desc string) error {
	if err := Init(); err != nil {
		return err
	}
	var e uintptr
	pipe := parseLaunch(desc, &e)
	if err := gerror(e); err != nil || pipe == 0 {
		return errors.Join(errors.New("gst: "+desc), err)
	}
	defer objectUnref(pipe)
	bus := elementGetBus(pipe)
	defer objectUnref(bus)
	elementSetState(pipe, statePlaying)
	defer elementSetState(pipe, stateNull)
	for {
		msg := busTimedPopFiltered(bus, 60e9, msgEOS|msgError)
		if msg == 0 {
			return errors.New("gst: timed out: " + desc)
		}
		t := messageType(msg)
		var err error
		if t == msgError {
			err = parseError(msg)
		}
		miniObjectUnref(msg)
		return err
	}
}

func parseError(msg uintptr) error {
	var e, debug uintptr
	messageParseError(msg, &e, &debug)
	err := gerror(e)
	cstring(debug, true)
	return err
}

// floats reads a field "name=(float){ a, b, … }" of a structure's text.
func floats(s, name string) []float64 {
	_, rest, ok := strings.Cut(s, name+"=(float){")
	if !ok {
		return nil
	}
	list, _, _ := strings.Cut(rest, "}")
	var out []float64
	for _, f := range strings.Split(list, ",") {
		var v float64
		if _, err := fmt.Sscan(strings.TrimSpace(f), &v); err == nil {
			out = append(out, v)
		}
	}
	return out
}
