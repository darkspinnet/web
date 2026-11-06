package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/scaleform"
)

func generateElementIcons(gamePath, outputPath string) error {
	file, err := os.Open(filepath.Join(gamePath, "Data", "FlashUI.package"))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	pkg, err := dbpf.NewReader(file, info.Size())
	if err != nil {
		return err
	}
	for _, entry := range pkg.Entries {
		// Planet_Room_I68.dds is the atlas referenced by Planet_Room's
		// NPC thumbnail movie (0xa0a9d54b).
		if entry.Type != scaleform.ImageResourceType || entry.Group != 0 || entry.Instance != 0xd840d445 {
			continue
		}
		reader, err := pkg.Open(entry)
		if err != nil {
			return err
		}
		payload, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		return writeElementIcons(payload, outputPath)
	}
	return fmt.Errorf("Planet_Room_I68.dds is missing from FlashUI.package")
}

func writeElementIcons(dds []byte, outputPath string) error {
	const headerSize, width, height = 128, 231, 195
	if len(dds) < headerSize+width*height*4 || string(dds[:4]) != "DDS " {
		return fmt.Errorf("invalid Planet Room icon atlas")
	}
	word := func(offset int) uint32 { return binary.LittleEndian.Uint32(dds[offset:]) }
	if word(4) != 124 || word(12) != height || word(16) != width || word(20) != width*4 ||
		word(84) != 0 || word(88) != 32 || word(92) != 0x00ff0000 || word(96) != 0x0000ff00 ||
		word(100) != 0x000000ff || word(104) != 0xff000000 {
		return fmt.Errorf("unexpected Planet Room icon atlas format")
	}
	if err := os.MkdirAll(outputPath, 0o755); err != nil {
		return err
	}
	// These 32-pixel regions match the movie's DefineSubImage tags.
	origins := map[string]image.Point{
		"bio": image.Pt(199, 92), "cyber": image.Pt(131, 92), "necro": image.Pt(199, 131),
		"plasma": image.Pt(195, 0), "quantum": image.Pt(195, 35),
	}
	for element, origin := range origins {
		icon := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				offset := headerSize + ((origin.Y+y)*width+origin.X+x)*4
				pixel := dds[offset : offset+4]
				icon.SetNRGBA(x, y, color.NRGBA{R: pixel[2], G: pixel[1], B: pixel[0], A: pixel[3]})
			}
		}
		file, err := os.Create(filepath.Join(outputPath, element+".png"))
		if err != nil {
			return err
		}
		encodeErr := png.Encode(file, icon)
		closeErr := file.Close()
		if encodeErr != nil {
			return encodeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
