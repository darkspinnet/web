package main

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestBarracudaPortraitUsesAuthoredCreatureID(t *testing.T) {
	// These fixed fields are from ZelemBasicRanged.Noun and the matching
	// PreBaked metadata. Its shared zlm_minn_sp_1 animation points elsewhere.
	noun := make([]byte, 200)
	binary.LittleEndian.PutUint64(noun[152:160], 0xd7b08f45)
	copy(noun[160:], "zlm_minn_sp_1_idle")
	asset, ok := npcCreatureAssetID(noun)
	if !ok || asset != 0xd7b08f45 {
		t.Fatalf("noun asset = %x, %v", asset, ok)
	}
	metadata, err := hex.DecodeString("0000000d00000000d7b08f452b978c4640626200650a89da")
	if err != nil {
		t.Fatal(err)
	}
	portraitAsset, ok := npcPortraitAssetID(metadata, npcPortraitGroup, 0x650a89da)
	if !ok || portraitAsset != asset {
		t.Fatalf("portrait asset = %x, %v; want %x", portraitAsset, ok, asset)
	}
	if _, ok := npcPortraitAssetID(metadata, npcPortraitGroup, 0x650a89d9); ok {
		t.Fatal("accepted the spider's resource key")
	}
}

func TestPortraitMetadataRejectsUnknownOrMismatchedIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]byte)
	}{
		{"unsupported version", func(b []byte) { binary.BigEndian.PutUint32(b, 12) }},
		{"different group", func(b []byte) { binary.BigEndian.PutUint32(b[16:], npcPortraitGroup+1) }},
		{"missing asset", func(b []byte) { binary.BigEndian.PutUint64(b[4:], 0) }},
		{"sentinel asset", func(b []byte) { binary.BigEndian.PutUint64(b[4:], ^uint64(0)) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			b, _ := hex.DecodeString("0000000d00000000d7b08f452b978c4640626200650a89da")
			test.change(b)
			if _, ok := npcPortraitAssetID(b, npcPortraitGroup, 0x650a89da); ok {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
	if _, ok := npcPortraitAssetID([]byte{0}, npcPortraitGroup, 0); ok {
		t.Fatal("accepted truncated metadata")
	}
	if _, ok := npcCreatureAssetID(make([]byte, 159)); ok {
		t.Fatal("accepted truncated noun")
	}
	if _, ok := npcCreatureAssetID(make([]byte, 160)); ok {
		t.Fatal("accepted absent creature asset")
	}
}
