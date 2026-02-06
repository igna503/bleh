package imageproc

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	"github.com/disintegration/imaging"
	dither "github.com/makeworld-the-better-one/dither"

	"bleh/internal/mxw01"
)

// DecodeImage loads an image from a given path or stdin ("-").
func DecodeImage(path string) (image.Image, error) {
	if path == "-" {
		return DecodeImageFromReader(os.Stdin)
	}
	img, err := imaging.Open(path, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("failed to open image %q: %v", path, err)
	}
	return img, nil
}

func DecodeImageFromReader(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}
	return img, nil
}

func PadImageToMinLines(img image.Image, minLines int) image.Image {
	bounds := img.Bounds()
	if bounds.Dy() >= minLines {
		return img
	}
	dst := imaging.New(bounds.Dx(), minLines, color.White)
	dst = imaging.Paste(dst, img, image.Pt(0, 0))
	return dst
}

func LoadImageMonoFromImage(img image.Image, ditherType string) ([]byte, int, error) {
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, 0, fmt.Errorf("invalid image bounds: %v", b)
	}
	ratio := float64(b.Dx()) / float64(b.Dy())
	height := int(float64(mxw01.LinePixels) / ratio)
	if height <= 0 {
		return nil, 0, fmt.Errorf("computed invalid height: %d", height)
	}

	img = imaging.Resize(img, mxw01.LinePixels, height, imaging.Lanczos)
	img = imaging.Grayscale(img)

	if ditherType != "none" {
		palette := []color.Color{color.Black, color.White}
		d := dither.NewDitherer(palette)
		switch ditherType {
		case "floyd":
			d.Matrix = dither.FloydSteinberg
		case "bayer2x2":
			d.Mapper = dither.Bayer(2, 2, 1.0)
		case "bayer4x4":
			d.Mapper = dither.Bayer(4, 4, 1.0)
		case "bayer8x8":
			d.Mapper = dither.Bayer(8, 8, 1.0)
		case "bayer16x16":
			d.Mapper = dither.Bayer(16, 16, 1.0)
		case "atkinson":
			d.Matrix = dither.Atkinson
		case "jjn":
			d.Matrix = dither.JarvisJudiceNinke
		default:
			return nil, 0, fmt.Errorf("unknown dither type: %s", ditherType)
		}
		img = d.DitherCopy(img)
	} else {
		img = imaging.AdjustContrast(img, 10)
	}

	pixels := make([]byte, (mxw01.LinePixels*height)/8)
	for y := 0; y < height; y++ {
		for x := 0; x < mxw01.LinePixels; x++ {
			gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if gray.Y < 128 {
				idx := (y*mxw01.LinePixels + x) / 8
				pixels[idx] |= 1 << (x % 8)
			}
		}
	}

	return pixels, height, nil
}

func LoadImage4BitFromImage(img image.Image, ditherType string) ([]byte, int, error) {
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, 0, fmt.Errorf("invalid image bounds: %v", b)
	}
	ratio := float64(b.Dx()) / float64(b.Dy())
	height := int(float64(mxw01.LinePixels) / ratio)
	if height <= 0 {
		return nil, 0, fmt.Errorf("computed invalid height: %d", height)
	}

	img = imaging.Resize(img, mxw01.LinePixels, height, imaging.Lanczos)
	img = imaging.Grayscale(img)

	palette := make([]color.Color, 16)
	for i := 0; i < 16; i++ {
		v := uint8(i * 17)
		palette[i] = color.Gray{Y: 255 - v}
	}

	if ditherType != "none" {
		d := dither.NewDitherer(palette)
		switch ditherType {
		case "floyd":
			d.Matrix = dither.FloydSteinberg
		case "bayer2x2":
			d.Mapper = dither.Bayer(2, 2, 0.2)
		case "bayer4x4":
			d.Mapper = dither.Bayer(4, 4, 0.2)
		case "bayer8x8":
			d.Mapper = dither.Bayer(8, 8, 0.2)
		case "bayer16x16":
			d.Mapper = dither.Bayer(16, 16, 0.2)
		case "atkinson":
			d.Matrix = dither.Atkinson
		case "jjn":
			d.Matrix = dither.JarvisJudiceNinke
		default:
			return nil, 0, fmt.Errorf("unknown dither type: %s", ditherType)
		}
		img = d.DitherCopy(img)
	}

	width := mxw01.LinePixels
	pixels := make([]byte, (width*height)/2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			level := (255 - gray.Y) >> 4
			idx := (y*width + x) >> 1
			shift := uint(((x & 1) ^ 1) << 2)
			pixels[idx] |= byte(level) << shift
		}
	}

	return pixels, height, nil
}

func RenderPreviewFrom1bpp(pixels []byte, width, height int) image.Image {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) / 8
			bit := uint(x % 8)
			if pixels[idx]&(1<<bit) != 0 {
				img.SetGray(x, y, color.Gray{Y: 0})
			} else {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return img
}

func RenderPreviewFrom4bpp(pixels []byte, width, height int) image.Image {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) >> 1
			shift := uint(((x & 1) ^ 1) << 2)
			val := (pixels[idx] >> shift) & 0x0F
			gray := 255 - val*17
			img.SetGray(x, y, color.Gray{Y: gray})
		}
	}
	return img
}
