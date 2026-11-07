package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/scaleform"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type mapArtwork struct {
	Image, Source string
	Crop          *struct{ X, Y, Width, Height int }
}

func generateMapArtwork(gamePath, manifest, outputPath string) error {
	data, err := os.ReadFile(filepath.Join("data", manifest))
	if err != nil {
		return err
	}
	var artworks map[string]mapArtwork
	if err = json.Unmarshal(data, &artworks); err != nil {
		return err
	}
	wanted := make(map[uint64][]mapArtwork, len(artworks))
	for _, artwork := range artworks {
		id := uint64(catalog.HashID(artwork.Source))
		wanted[id] = append(wanted[id], artwork)
	}
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
	if err = os.MkdirAll(outputPath, 0o755); err != nil {
		return err
	}
	// Planet images follow GalaxyMap sprite 78's labels. Level previews follow
	// FE_MM2b sprite 361's clip1–clip24 labels and their DefineSubImage bounds.
	for _, entry := range pkg.Entries {
		images, found := wanted[entry.Instance]
		if !found || entry.Type != scaleform.ImageResourceType || entry.Group != 0 {
			continue
		}
		reader, err := pkg.Open(entry)
		if err != nil {
			return err
		}
		dds, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		for _, artwork := range images {
			name := filepath.Base(artwork.Image)
			if err = writeMapArtwork(dds, filepath.Join(outputPath, name), artwork); err != nil {
				return fmt.Errorf("map artwork %s: %w", name, err)
			}
		}
		delete(wanted, entry.Instance)
	}
	if len(wanted) != 0 {
		return fmt.Errorf("%d images from %s missing in FlashUI.package", len(wanted), manifest)
	}
	return nil
}

func writeMapArtwork(dds []byte, path string, artwork mapArtwork) error {
	if len(dds) < 128 || string(dds[:4]) != "DDS " {
		return fmt.Errorf("invalid map texture")
	}
	word := func(offset int) uint32 { return binary.LittleEndian.Uint32(dds[offset:]) }
	width, height := int(word(16)), int(word(12))
	if width < 1 || height < 1 || width > 4096 || height > 4096 || len(dds) != 128+width*height*4 {
		return fmt.Errorf("unexpected map texture dimensions")
	}
	if word(4) != 124 || word(20) != uint32(width*4) ||
		word(84) != 0 || word(88) != 32 || word(92) != 0xff0000 ||
		word(96) != 0xff00 || word(100) != 0xff || word(104) != 0xff000000 {
		return fmt.Errorf("unsupported map texture format")
	}
	planet := image.NewNRGBA(image.Rect(0, 0, width, height))
	bounds := image.Rectangle{}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := 128 + (y*width+x)*4
			pixel := dds[offset : offset+4]
			planet.SetNRGBA(x, y, color.NRGBA{R: pixel[2], G: pixel[1], B: pixel[0], A: pixel[3]})
			if pixel[3] != 0 {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if crop := artwork.Crop; crop != nil {
		bounds = image.Rect(crop.X, crop.Y, crop.X+crop.Width, crop.Y+crop.Height)
		if !bounds.In(planet.Bounds()) {
			return fmt.Errorf("map image crop is outside its texture")
		}
	}
	if bounds.Empty() {
		return fmt.Errorf("empty planet image")
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(file, planet.SubImage(bounds))
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}
