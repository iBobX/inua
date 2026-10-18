package kdeconnect

import "testing"

// KDE Connect for Android writes some numbers of its identity as strings.
func TestIdentityNumbersAsStrings(t *testing.T) {
	line := []byte(`{"id":1792290157531,"type":"kdeconnect.identity","body":{"deviceId":"3c9b518b62b54d79bd64c57e9e223793","deviceName":"Pixel 8a","protocolVersion":8,"deviceType":"phone","tcpPort":"1716","targetDeviceId":"abc","targetProtocolVersion":"8"}}`)
	var p Packet
	var d DeviceInfo
	if err := jsonPacket(line, &p, &d); err != nil {
		t.Fatal(err)
	}
	if d.DeviceID != "3c9b518b62b54d79bd64c57e9e223793" || d.DeviceName != "Pixel 8a" || d.DeviceType != "phone" ||
		d.ProtocolVersion != 8 || d.TCPPort != 1716 || d.TargetDeviceID != "abc" || d.TargetProtocolVersion != 8 {
		t.Fatalf("read as %+v", d)
	}
	if err := jsonPacket([]byte(`{"type":"kdeconnect.identity","body":{"protocolVersion":"eight"}}`), &p, &d); err == nil {
		t.Fatal("a version that is not a number was taken")
	}
}
