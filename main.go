package main

import (
	"bytes"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/loggo"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/image/draw"
)

func main() {
	defer common.CrashLog()

	src := flag.String("src", "", "src image path")
	target := flag.String("target", "", "target image path")
	lib := flag.String("lib", "", "image lib path")
	worker := flag.Int("worker", 12, "worker thread num")
	database := flag.String("database", "./database.bin", "cache database")
	pixelsize := flag.Int("pixelsize", 64, "pic scale size per one pixel")
	scalealg := flag.String("scalealg", "CatmullRom", "pic scale function NearestNeighbor/ApproxBiLinear/BiLinear/CatmullRom")
	checkhash := flag.Bool("checkhash", true, "check database pic hash")
	maxsize := flag.Int("maxsize", 4, "pic max size in GB")
	libname := flag.String("libname", "default", "image lib name in database")
	srcsize := flag.Int("srcsize", 128, "src image auto scale pixel size")

	flag.Parse()

	if *src == "" || *target == "" || *lib == "" {
		fmt.Println("need src target lib")
		flag.Usage()
		os.Exit(1)
	}
	if getScaler(*scalealg) == nil {
		fmt.Println("scalealg type error")
		flag.Usage()
		os.Exit(1)
	}
	targetLower := strings.ToLower(*target)
	if !strings.HasSuffix(targetLower, ".png") &&
		!strings.HasSuffix(targetLower, ".jpg") &&
		!strings.HasSuffix(targetLower, ".jpeg") {
		fmt.Println("target type error, png/jpg")
		flag.Usage()
		os.Exit(1)
	}

	level := loggo.LEVEL_INFO
	loggo.Ini(loggo.Config{
		Level:  level,
		Prefix: "mosaic",
		MaxDay: 3,
	})
	loggo.Info("start...")

	loggo.Info("src %s", *src)
	loggo.Info("target %s", *target)
	loggo.Info("lib %s", *lib)

	srcimg, cachemap, err := parseSrc(*src, *scalealg, *srcsize)
	if err != nil {
		loggo.Error("parse_src failed: %v", err)
		os.Exit(1)
	}
	err = loadLib(*lib, *worker, *database, *pixelsize, *scalealg, *checkhash, *libname)
	if err != nil {
		loggo.Error("load_lib failed: %v", err)
		os.Exit(1)
	}
	err = genTarget(srcimg, *target, *worker, *database, *pixelsize, *maxsize, *scalealg, *libname, cachemap)
	if err != nil {
		loggo.Error("gen_target failed: %v", err)
		os.Exit(1)
	}
}

type CacheInfo struct {
	num  int
	img  []image.Image
	lock sync.RWMutex
}

func parseSrc(src string, scalealg string, srcsize int) (image.Image, *sync.Map, error) {
	loggo.Info("parse_src %s", src)

	reader, err := os.Open(src)
	if err != nil {
		loggo.Error("parse_src Open fail %s %s", src, err)
		return nil, nil, err
	}
	defer reader.Close()

	fi, err := reader.Stat()
	if err != nil {
		loggo.Error("parse_src Stat fail %s %s", src, err)
		return nil, nil, err
	}
	filesize := fi.Size()

	img, _, err := image.Decode(reader)
	if err != nil {
		loggo.Error("parse_src Decode image fail %s %s", src, err)
		return nil, nil, err
	}

	scale := getScaler(scalealg)

	lenx := img.Bounds().Dx()
	leny := img.Bounds().Dy()
	maxDim := common.MaxOfInt(lenx, leny)
	if maxDim > srcsize {
		newlenx := lenx * srcsize / maxDim
		newleny := leny * srcsize / maxDim
		rect := image.Rectangle{image.Point{0, 0}, image.Point{newlenx, newleny}}
		dst := image.NewRGBA(rect)
		scale.Scale(dst, rect, img, img.Bounds(), draw.Over, nil)
		img = dst
	}

	bounds := img.Bounds()
	startx := bounds.Min.X
	starty := bounds.Min.Y
	endx := bounds.Max.X
	endy := bounds.Max.Y

	pixelnum := make(map[string]int)
	for y := starty; y < endy; y++ {
		for x := startx; x < endx; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r, g, b = r>>8, g>>8, b>>8
			pixelnum[makeString(uint8(r), uint8(g), uint8(b))]++
		}
	}

	var cachemap sync.Map
	top := 0
	num := 0
	for {
		maxpixel := ""
		maxpixelnum := 0
		for k, v := range pixelnum {
			if v > maxpixelnum {
				maxpixelnum = v
				maxpixel = k
			}
		}
		if maxpixelnum >= 16 {
			cachemap.Store(maxpixel, &CacheInfo{num: maxpixelnum})
			num++
		} else {
			break
		}
		if maxpixelnum > top {
			top = maxpixelnum
		}
		pixelnum[maxpixel] = 0
	}

	loggo.Info("parse_src cache top pixel num=%d max=%d", num, top)
	for i := 2; i <= top; i++ {
		cachemap.Range(func(key, value interface{}) bool {
			ci := value.(*CacheInfo)
			if ci.num == i {
				loggo.Info("parse_src cache top pixel [%s]=%d", key, i)
			}
			return true
		})
	}

	loggo.Info("parse_src ok %s %d %d*%d", src, filesize, img.Bounds().Dx(), img.Bounds().Dy())
	return img, &cachemap, nil
}

func getScaler(scalealg string) draw.Scaler {
	switch scalealg {
	case "NearestNeighbor":
		return draw.NearestNeighbor
	case "ApproxBiLinear":
		return draw.ApproxBiLinear
	case "BiLinear":
		return draw.BiLinear
	case "CatmullRom":
		return draw.CatmullRom
	default:
		return nil
	}
}

type FileInfo struct {
	Filename string
	R        uint8
	G        uint8
	B        uint8
	Hash     string
}


type ColorData struct {
	file int
	r    uint8
	g    uint8
	b    uint8
}

func loadLib(lib string, workernum int, database string, pixelsize int, scalealg string, checkhash bool, libname string) error {
	loggo.Info("load_lib %s", lib)

	loggo.Info("load_lib start ini database")
	colordata := make([]ColorData, 256*256*256)
	for i := 0; i <= 255; i++ {
		for j := 0; j <= 255; j++ {
			for z := 0; z <= 255; z++ {
				k := makeKey(uint8(i), uint8(j), uint8(z))
				colordata[k] = ColorData{r: uint8(i), g: uint8(j), b: uint8(z)}
			}
		}
	}

	loggo.Info("load_lib ini database ok")
	loggo.Info("load_lib start load database")

	db, err := bolt.Open(database, 0600, nil)
	if err != nil {
		loggo.Error("load_lib Open database fail %s %s", database, err)
		return err
	}
	defer db.Close()

	bucketName := "FileInfo" + libname + strconv.Itoa(pixelsize)

	dbtotal := 0
	_ = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			loggo.Error("load_lib Open database CreateBucketIfNotExists fail %s %s %s", database, bucketName, err)
			os.Exit(1)
		}
		b := tx.Bucket([]byte(bucketName))
		_ = b.ForEach(func(k, v []byte) error {
			dbtotal++
			return nil
		})
		return nil
	})

	lastload := time.Now()
	beginload := time.Now()
	var doneload atomic.Int32
	var doneloadsize atomic.Int64
	var lock sync.Mutex
	_ = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))

		needDel := make([]string, 0)

		type LoadFileInfo struct {
			k, v []byte
		}

		wp := NewWorkerPool(workernum, workernum*4, func(lf LoadFileInfo) {
			defer doneload.Add(1)

			bBuf := bytes.NewBuffer(lf.v)
			dec := gob.NewDecoder(bBuf)
			var fi FileInfo
			err = dec.Decode(&fi)
			if err != nil {
				loggo.Error("load_lib Open database Decode fail %s %s %s", database, string(lf.k), err)
				lock.Lock()
				needDel = append(needDel, string(lf.k))
				lock.Unlock()
				return
			}

			osfi, err := os.Stat(fi.Filename)
			if err != nil && os.IsNotExist(err) {
				loggo.Error("load_lib Open Filename IsNotExist, need delete %s %s %s", database, fi.Filename, err)
				lock.Lock()
				needDel = append(needDel, string(lf.k))
				lock.Unlock()
				return
			}

			doneloadsize.Add(osfi.Size())

			if checkhash {
				data, err := os.ReadFile(fi.Filename)
				if err != nil {
					loggo.Error("load_lib ReadFile fail %s %s %s", database, fi.Filename, err)
					return
				}

				hashstr := common.GetXXHashString(string(data))
				if hashstr != fi.Hash {
					loggo.Error("load_lib hash diff need delete %s %s %s %s", database, fi.Filename, hashstr, fi.Hash)
					lock.Lock()
					needDel = append(needDel, string(lf.k))
					lock.Unlock()
					return
				}
			}
		})

		_ = b.ForEach(func(k, v []byte) error {
			wp.Submit(LoadFileInfo{k, v})

			if time.Since(lastload) >= time.Second {
				lastload = time.Now()
				elapsedSec := float64(time.Since(beginload)) / float64(time.Second)
				curDone := doneload.Load()
				speed := float64(curDone) / elapsedSec
				left := ""
				if speed > 0 {
					left = time.Duration(int64(float64(dbtotal-int(curDone))/speed) * int64(time.Second)).String()
				}
				donesizem := doneloadsize.Load() / 1024 / 1024
				dataspeed := int(float64(donesizem) / elapsedSec)
				loggo.Info("load speed=%.2f/s percent=%d%% time=%s progress=%d/%d data=%dM dataspeed=%dM/s",
					speed, int(curDone)*100/dbtotal, left, curDone, dbtotal, donesizem, dataspeed)
			}

			return nil
		})

		wp.Stop()

		for _, k := range needDel {
			_ = b.Delete([]byte(k))
		}

		return nil
	})

	loggo.Info("load_lib load database ok")
	loggo.Info("load_lib start get image file list")
	imagefilelist := make([]string, 0)
	cached := 0
	_ = filepath.Walk(lib, func(path string, f os.FileInfo, err error) error {
		if f == nil || f.IsDir() {
			return nil
		}

		lowerName := strings.ToLower(f.Name())
		if !strings.HasSuffix(lowerName, ".jpeg") &&
			!strings.HasSuffix(lowerName, ".jpg") &&
			!strings.HasSuffix(lowerName, ".png") &&
			!strings.HasSuffix(lowerName, ".gif") {
			return nil
		}

		abspath, err := filepath.Abs(path)
		if err != nil {
			loggo.Error("load_lib get Abs fail %s %s %s", database, path, err)
			return nil
		}

		_ = db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucketName))
			v := b.Get([]byte(abspath))
			if v == nil {
				imagefilelist = append(imagefilelist, abspath)
			} else {
				cached++
			}
			return nil
		})

		return nil
	})

	loggo.Info("load_lib get image file list ok %d cache %d", len(imagefilelist), cached)
	loggo.Info("load_lib start calc image avg color %d", len(imagefilelist))
	var worker atomic.Int32
	begin := time.Now()
	last := time.Now()
	var done atomic.Int32
	var donesize atomic.Int64

	results := make(chan FileInfo, workernum*4)
	var saveCount atomic.Int32
	var saveWg sync.WaitGroup
	saveWg.Add(1)
	go func() {
		defer saveWg.Done()
		saveToDatabase(results, db, &saveCount, bucketName)
	}()

	scale := getScaler(scalealg)

	wp := NewWorkerPool(workernum, workernum*4, func(filename string) {
		calcAvgColor(filename, &worker, &done, &donesize, scale, pixelsize, results)
	})

	for _, filename := range imagefilelist {
		wp.Submit(filename)
		if time.Since(last) >= time.Second {
			last = time.Now()
			elapsedSec := float64(time.Since(begin)) / float64(time.Second)
			curDone := done.Load()
			speed := float64(curDone) / elapsedSec
			left := ""
			if speed > 0 {
				left = time.Duration(int64(float64(len(imagefilelist)-int(curDone))/speed) * int64(time.Second)).String()
			}
			donesizem := donesize.Load() / 1024 / 1024
			dataspeed := int(float64(donesizem) / elapsedSec)
			loggo.Info("calc speed=%.2f/s percent=%d%% time=%s progress=%d/%d saved=%d data=%dM dataspeed=%dM/s",
				speed, int(curDone)*100/len(imagefilelist), left, curDone, len(imagefilelist), saveCount.Load(), donesizem, dataspeed)
		}
	}
	wp.Stop()
	close(results)
	saveWg.Wait()

	loggo.Info("load_lib calc image avg color ok %d %d", len(imagefilelist), done.Load())
	loggo.Info("load_lib start save image avg color")

	maxcolornum := 0
	totalnum := 0
	_ = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))

		_ = b.ForEach(func(k, v []byte) error {
			bBuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(bBuf)
			var fi FileInfo
			err = dec.Decode(&fi)
			if err != nil {
				loggo.Error("load_lib Open database Decode fail %s %s %s", database, string(k), err)
				return nil
			}

			key := makeKey(fi.R, fi.G, fi.B)
			colordata[key].file++
			if colordata[key].file > maxcolornum {
				maxcolornum = colordata[key].file
			}
			totalnum++
			return nil
		})

		return nil
	})

	loggo.Info("load_lib save image avg color ok total %d max %d", totalnum, maxcolornum)

	if totalnum <= 0 {
		loggo.Error("load_lib no pic in lib %s", database)
		return errors.New("no pic")
	}

	tmpcolornum := make(map[int]int)
	tmpcolorone := make(map[int]ColorData)
	colorgroup := []struct {
		name string
		c    color.RGBA
		num  int
	}{
		{"Black", common.Black, 0},
		{"White", common.White, 0},
		{"Red", common.Red, 0},
		{"Lime", common.Lime, 0},
		{"Blue", common.Blue, 0},
		{"Yellow", common.Yellow, 0},
		{"Cyan", common.Cyan, 0},
		{"Magenta", common.Magenta, 0},
		{"Silver", common.Silver, 0},
		{"Gray", common.Gray, 0},
		{"Maroon", common.Maroon, 0},
		{"Olive", common.Olive, 0},
		{"Green", common.Green, 0},
		{"Purple", common.Purple, 0},
		{"Teal", common.Teal, 0},
		{"Navy", common.Navy, 0},
	}

	for _, data := range colordata {
		tmpcolornum[data.file]++
		tmpcolorone[data.file] = data

		if data.file > 0 {
			min := 0
			mindistance := math.MaxFloat64
			for index, cg := range colorgroup {
				diff := common.ColorDistance(color.RGBA{data.r, data.g, data.b, 0}, cg.c)
				if diff < mindistance {
					min = index
					mindistance = diff
				}
			}

			colorgroup[min].num += data.file
		}
	}

	for i := 0; i <= maxcolornum; i++ {
		str := ""
		if tmpcolornum[i] == 1 {
			str = makeString(tmpcolorone[i].r, tmpcolorone[i].g, tmpcolorone[i].b)
		}
		loggo.Info("load_lib avg color num distribution %d = %d %s", i, tmpcolornum[i], str)
	}

	maxcolorgroupnum := 0
	maxcolorgroupindex := 0
	for index, cg := range colorgroup {
		loggo.Info("load_lib avg color color distribution %s = %d", cg.name, cg.num)
		if cg.num > maxcolorgroupnum {
			maxcolorgroupnum = cg.num
			maxcolorgroupindex = index
		}
	}
	loggo.Info("load_lib avg color color max %s %d", colorgroup[maxcolorgroupindex].name, colorgroup[maxcolorgroupindex].num)

	return nil
}

func makeKey(r uint8, g uint8, b uint8) int {
	return int(r)*256*256 + int(g)*256 + int(b)
}

func makeString(r uint8, g uint8, b uint8) string {
	return "r " + strconv.Itoa(int(r)) + " g " + strconv.Itoa(int(g)) + " b " + strconv.Itoa(int(b))
}

func calcImg(src image.Image, filename string, scaler draw.Scaler, pixelsize int) (image.Image, error) {
	bounds := src.Bounds()

	minDim := common.MinOfInt(bounds.Dx(), bounds.Dy())
	startx := bounds.Min.X + (bounds.Dx()-minDim)/2
	starty := bounds.Min.Y + (bounds.Dy()-minDim)/2
	endx := common.MinOfInt(startx+minDim, bounds.Max.X)
	endy := common.MinOfInt(starty+minDim, bounds.Max.Y)

	if startx != bounds.Min.X || starty != bounds.Min.Y || endx != bounds.Max.X || endy != bounds.Max.Y {
		dst := image.NewRGBA(image.Rectangle{image.Point{0, 0}, image.Point{minDim, minDim}})
		draw.Copy(dst, image.Point{0, 0}, src, image.Rectangle{image.Point{startx, starty}, image.Point{endx, endy}}, draw.Over, nil)
		src = dst
	}

	bounds = src.Bounds()
	if bounds.Dx() != bounds.Dy() {
		loggo.Error("calc_img crop image fail %s %d %d", filename, bounds.Dx(), bounds.Dy())
		return nil, errors.New("bounds error")
	}

	minDim = common.MinOfInt(bounds.Dx(), bounds.Dy())
	if minDim < pixelsize {
		loggo.Error("calc_img image too small %s %d %d", filename, minDim, pixelsize)
		return nil, errors.New("too small")
	}

	if minDim > pixelsize {
		rect := image.Rectangle{image.Point{0, 0}, image.Point{pixelsize, pixelsize}}
		dst := image.NewRGBA(rect)
		scaler.Scale(dst, rect, src, src.Bounds(), draw.Over, nil)
		src = dst
	}

	return src, nil
}

func calcAvgColor(filename string, worker *atomic.Int32, done *atomic.Int32, donesize *atomic.Int64, scaler draw.Scaler, pixelsize int, results chan<- FileInfo) {
	defer common.CrashLog()
	defer worker.Add(-1)
	defer done.Add(1)

	data, err := os.ReadFile(filename)
	if err != nil {
		loggo.Error("calc_avg_color ReadFile fail %s %s", filename, err)
		return
	}
	donesize.Add(int64(len(data)))

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		loggo.Error("calc_avg_color Decode image fail %s %s", filename, err)
		return
	}

	img, err = calcImg(img, filename, scaler, pixelsize)
	if err != nil {
		loggo.Error("calc_avg_color calc_img image fail %s %s", filename, err)
		return
	}

	bounds := img.Bounds()
	var sumR, sumG, sumB, count float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r, g, b = r>>8, g>>8, b>>8
			sumR += float64(r)
			sumG += float64(g)
			sumB += float64(b)
			count++
		}
	}

	results <- FileInfo{
		Filename: filename,
		R:        uint8(sumR / count),
		G:        uint8(sumG / count),
		B:        uint8(sumB / count),
		Hash:     common.GetXXHashString(string(data)),
	}
}

func saveToDatabase(results <-chan FileInfo, db *bolt.DB, saveCount *atomic.Int32, bucketName string) {
	defer common.CrashLog()

	const batchSize = 50
	var batch []FileInfo

	flush := func() {
		if len(batch) == 0 {
			return
		}
		_ = db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucketName))
			for _, fi := range batch {
				var buf bytes.Buffer
				enc := gob.NewEncoder(&buf)
				if err := enc.Encode(&fi); err != nil {
					loggo.Error("save_to_database Encode FileInfo fail %s %s", fi.Filename, err)
					continue
				}
				_ = b.Put([]byte(fi.Filename), buf.Bytes())
			}
			return nil
		})
		saveCount.Add(int32(len(batch)))
		batch = batch[:0]
	}

	for fi := range results {
		batch = append(batch, fi)
		if len(batch) >= batchSize {
			flush()
		}
	}
	flush()
}

func genTarget(srcimg image.Image, target string, workernum int, database string, pixelsize int, maxsize int, scalealg string, libname string, cachemap *sync.Map) error {
	loggo.Info("gen_target %s", target)

	db, err := bolt.Open(database, 0600, nil)
	if err != nil {
		loggo.Error("gen_target Open database fail %s %s", database, err)
		return err
	}
	defer db.Close()

	bucketName := "FileInfo" + libname + strconv.Itoa(pixelsize)
	bounds := srcimg.Bounds()

	startx := bounds.Min.X
	starty := bounds.Min.Y
	endx := bounds.Max.X
	endy := bounds.Max.Y

	last := time.Now()
	begin := time.Now()
	total := bounds.Dx() * bounds.Dy()
	var done atomic.Int32
	var cached atomic.Int32

	lenx := bounds.Dx() * pixelsize
	leny := bounds.Dy() * pixelsize

	outputfilesize := lenx * leny * 4 / 1024 / 1024 / 1024
	if outputfilesize > maxsize {
		loggo.Error("gen_target too big %s %dG than %dG", target, outputfilesize, maxsize)
		return errors.New("too big")
	}

	loggo.Info("gen_target start gen pixel %s %dG max %dG", target, outputfilesize, maxsize)

	dst := image.NewRGBA(image.Rectangle{image.Point{0, 0}, image.Point{lenx, leny}})

	type GenInfo struct {
		x int
		y int
		c color.RGBA
	}

	wp := NewWorkerPool(workernum, workernum*4, func(gi GenInfo) {
		defer done.Add(1)
		genTargetPixel(gi.c, gi.x, gi.y, dst, db, bucketName, pixelsize, scalealg, cachemap, &cached)
	})

	for y := starty; y < endy; y++ {
		for x := startx; x < endx; x++ {
			r, g, b, _ := srcimg.At(x, y).RGBA()
			r, g, b = r>>8, g>>8, b>>8

			wp.Submit(GenInfo{x: x, y: y, c: color.RGBA{uint8(r), uint8(g), uint8(b), 0}})

			if time.Since(last) >= time.Second {
				last = time.Now()
				elapsedSec := float64(time.Since(begin)) / float64(time.Second)
				curDone := done.Load()
				speed := float64(curDone) / elapsedSec
				left := ""
				if speed > 0 {
					left = time.Duration(int64(float64(total-int(curDone))/speed) * int64(time.Second)).String()
				}
				curCached := cached.Load()
				loggo.Info("gen speed=%.2f/s percent=%d%% time=%s progress=%d/%d cached=%d cached-percent=%d%%",
					speed, int(curDone)*100/total, left, curDone, total, curCached, int(curCached)*100/total)
			}
		}
	}

	wp.Stop()

	loggo.Info("gen_target gen pixel ok %s", target)
	loggo.Info("gen_target start write file %s", target)
	dstFile, err := os.Create(target)
	if err != nil {
		loggo.Error("gen_target Create fail %s %s", target, err)
		return err
	}
	defer dstFile.Close()

	targetLower := strings.ToLower(target)
	if strings.HasSuffix(targetLower, ".png") {
		err = png.Encode(dstFile, dst)
	} else if strings.HasSuffix(targetLower, ".jpg") || strings.HasSuffix(targetLower, ".jpeg") {
		err = jpeg.Encode(dstFile, dst, &jpeg.Options{Quality: 100})
	}
	if err != nil {
		loggo.Error("gen_target Encode fail %s %s", target, err)
		return err
	}

	loggo.Info("gen_target write file ok %s", target)
	return nil
}

func findMatchingImages(db *bolt.DB, bucketName string, scalealg string, pixelsize int, src color.RGBA) []image.Image {
	mindiff := math.MaxFloat64
	var mindiffnames []string
	var minfi FileInfo

	_ = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		_ = b.ForEach(func(k, v []byte) error {
			bBuf := bytes.NewBuffer(v)
			dec := gob.NewDecoder(bBuf)
			var fi FileInfo
			err := dec.Decode(&fi)
			if err != nil {
				loggo.Error("gen_target_pixel database Decode fail %s %s", string(k), err)
				os.Exit(1)
			}

			if minfi.R == fi.R && minfi.G == fi.G && minfi.B == fi.B {
				mindiffnames = append(mindiffnames, fi.Filename)
				return nil
			}

			tmp := color.RGBA{fi.R, fi.G, fi.B, 0}
			diff := common.ColorDistance(src, tmp)
			if diff < mindiff {
				mindiff = diff
				mindiffnames = mindiffnames[:0]
				mindiffnames = append(mindiffnames, fi.Filename)
				minfi = fi
			}

			return nil
		})
		return nil
	})

	var result []image.Image
	scale := getScaler(scalealg)
	for _, mindiffname := range mindiffnames {
		reader, err := os.Open(mindiffname)
		if err != nil {
			loggo.Error("gen_target_pixel Open fail %s %s", mindiffname, err)
			os.Exit(1)
		}

		minimg, _, err := image.Decode(reader)
		_ = reader.Close()
		if err != nil {
			loggo.Error("gen_target_pixel Decode fail %s %s", mindiffname, err)
			continue
		}

		minimg, err = calcImg(minimg, mindiffname, scale, pixelsize)
		if err != nil {
			loggo.Error("gen_target_pixel calc_img image fail %s %s", mindiffname, err)
			continue
		}

		result = append(result, minimg)
	}
	return result
}

func genTargetPixel(src color.RGBA, x int, y int, dst *image.RGBA, db *bolt.DB, bucketName string, pixelsize int, scalealg string, cachemap *sync.Map, cached *atomic.Int32) {
	var minimgs []image.Image

	key := makeString(src.R, src.G, src.B)
	v, ok := cachemap.Load(key)
	if ok {
		ci := v.(*CacheInfo)
		ci.lock.RLock()
		minimgs = ci.img
		ci.lock.RUnlock()
	}

	if len(minimgs) == 0 {
		if ok {
			ci := v.(*CacheInfo)
			ci.lock.Lock()
			minimgs = ci.img
			if len(minimgs) == 0 {
				minimgs = findMatchingImages(db, bucketName, scalealg, pixelsize, src)
				ci.img = minimgs
			} else {
				cached.Add(1)
			}
			ci.lock.Unlock()
		} else {
			minimgs = findMatchingImages(db, bucketName, scalealg, pixelsize, src)
		}
	} else {
		cached.Add(1)
	}

	if len(minimgs) == 0 {
		return
	}

	minimg := minimgs[common.RandInt31n(len(minimgs))]

	if common.RandInt()%2 == 0 {
		bounds := minimg.Bounds()
		flippedImg := image.NewRGBA(bounds)
		dx := bounds.Dx()
		dy := bounds.Dy()
		for j := 0; j < dy; j++ {
			for i := 0; i < dx; i++ {
				flippedImg.Set((dx-1)-i, j, minimg.At(bounds.Min.X+i, bounds.Min.Y+j))
			}
		}
		minimg = flippedImg
	}

	draw.Copy(dst, image.Point{x * pixelsize, y * pixelsize}, minimg, minimg.Bounds(), draw.Over, nil)
}
