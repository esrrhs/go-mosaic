package main

import (
	"bytes"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"github.com/boltdb/bolt"
	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/loggo"
	"github.com/esrrhs/gohome/threadpool"
	"golang.org/x/image/draw"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {

	defer common.CrashLog()

	src := flag.String("src", "", "src image path")
	target := flag.String("target", "", "target image path")
	lib := flag.String("lib", "", "image lib path")
	worker := flag.Int("worker", 12, "worker thread num")
	database := flag.String("database", "./database.bin", "cache datbase")
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
		return
	}
	if getScaler(*scalealg) == nil {
		fmt.Println("scalealg type error")
		flag.Usage()
		return
	}
	if !strings.HasSuffix(strings.ToLower(*target), ".png") &&
		!strings.HasSuffix(strings.ToLower(*target), ".jpg") {
		fmt.Println("target type error, png/jpg")
		flag.Usage()
		return
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

	err, srcimg, cachemap := parse_src(*src, *scalealg, *srcsize)
	if err != nil {
		return
	}
	err = load_lib(*lib, *worker, *database, *pixelsize, *scalealg, *checkhash, *libname)
	if err != nil {
		return
	}
	err = gen_target(srcimg, *target, *worker, *database, *pixelsize, *maxsize, *scalealg, *libname, cachemap)
	if err != nil {
		return
	}
}

type CacheInfo struct {
	num  int
	img  []image.Image
	lock sync.Mutex
}

func parse_src(src string, scalealg string, srcsize int) (error, image.Image, *sync.Map) {
	loggo.Info("parse_src %s", src)

	reader, err := os.Open(src)
	if err != nil {
		loggo.Error("parse_src Open fail %s %s", src, err)
		return err, nil, nil
	}
	defer reader.Close()

	fi, err := reader.Stat()
	if err != nil {
		loggo.Error("parse_src Stat fail %s %s", src, err)
		return err, nil, nil
	}
	filesize := fi.Size()

	img, _, err := image.Decode(reader)
	if err != nil {
		loggo.Error("parse_src Decode image fail %s %s", src, err)
		return err, nil, nil
	}

	scale := getScaler(scalealg)

	lenx := img.Bounds().Dx()
	leny := img.Bounds().Dy()
	len := common.MaxOfInt(lenx, leny)
	if len > srcsize {
		newlenx := lenx * srcsize / len
		newleny := leny * srcsize / len
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

			pixelnum[make_string(uint8(r), uint8(g), uint8(b))]++
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
	return nil, img, &cachemap
}

func getScaler(scalealg string) draw.Scaler {
	var scale draw.Scaler
	if scalealg == "NearestNeighbor" {
		scale = draw.NearestNeighbor
	} else if scalealg == "ApproxBiLinear" {
		scale = draw.ApproxBiLinear
	} else if scalealg == "BiLinear" {
		scale = draw.BiLinear
	} else if scalealg == "CatmullRom" {
		scale = draw.CatmullRom
	}
	return scale
}

type FileInfo struct {
	Filename string
	R        uint8
	G        uint8
	B        uint8
	Hash     string
}

type CalFileInfo struct {
	fi   FileInfo
	ok   bool
	done bool
}

type ColorData struct {
	file int
	r    uint8
	g    uint8
	b    uint8
}

func load_lib(lib string, workernum int, database string, pixelsize int, scalealg string, checkhash bool, libname string) error {
	loggo.Info("load_lib %s", lib)

	loggo.Info("load_lib start ini database")
	var colordata []ColorData
	for i := 0; i <= 255; i++ {
		for j := 0; j <= 255; j++ {
			for z := 0; z <= 255; z++ {
				colordata = append(colordata, ColorData{})
			}
		}
	}

	for i := 0; i <= 255; i++ {
		for j := 0; j <= 255; j++ {
			for z := 0; z <= 255; z++ {
				k := make_key(uint8(i), uint8(j), uint8(z))
				colordata[k].r, colordata[k].g, colordata[k].b = uint8(i), uint8(j), uint8(z)
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

	bucket_name := "FileInfo" + libname + strconv.Itoa(pixelsize)

	dbtotal := 0
	db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucket_name))
		if err != nil {
			loggo.Error("load_lib Open database CreateBucketIfNotExists fail %s %s %s", database, bucket_name, err)
			os.Exit(1)
		}
		b := tx.Bucket([]byte(bucket_name))
		b.ForEach(func(k, v []byte) error {
			dbtotal++
			return nil
		})
		return nil
	})

	lastload := time.Now()
	beginload := time.Now()
	var doneload int32
	var loading int32
	var doneloadsize int64
	var lock sync.Mutex
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket_name))

		need_del := make([]string, 0)

		type LoadFileInfo struct {
			k, v []byte
		}

		tp := threadpool.NewThreadPool(workernum, 16, func(in interface{}) {
			defer atomic.AddInt32(&doneload, 1)
			defer atomic.AddInt32(&loading, -1)

			lf := in.(LoadFileInfo)

			var b bytes.Buffer
			b.Write(lf.v)

			dec := gob.NewDecoder(&b)
			var fi FileInfo
			err = dec.Decode(&fi)
			if err != nil {
				loggo.Error("load_lib Open database Decode fail %s %s %s", database, string(lf.k), err)
				lock.Lock()
				defer lock.Unlock()
				need_del = append(need_del, string(lf.k))
				return
			}

			osfi, err := os.Stat(fi.Filename)
			if err != nil && os.IsNotExist(err) {
				loggo.Error("load_lib Open Filename IsNotExist, need delete %s %s %s", database, fi.Filename, err)
				lock.Lock()
				defer lock.Unlock()
				need_del = append(need_del, string(lf.k))
				return
			}

			defer atomic.AddInt64(&doneloadsize, osfi.Size())

			if checkhash {
				reader, err := os.Open(fi.Filename)
				if err != nil {
					loggo.Error("load_lib Open fail %s %s %s", database, fi.Filename, err)
					return
				}
				defer reader.Close()

				bytes, err := ioutil.ReadAll(reader)
				if err != nil {
					loggo.Error("load_lib ReadAll fail %s %s %s", database, fi.Filename, err)
					return
				}

				hashstr := common.GetXXHashString(string(bytes))

				if hashstr != fi.Hash {
					loggo.Error("load_lib hash diff need delete %s %s %s %s", database, fi.Filename, hashstr, fi.Hash)
					lock.Lock()
					defer lock.Unlock()
					need_del = append(need_del, string(lf.k))
					return
				}
			}
		})

		b.ForEach(func(k, v []byte) error {

			for {
				ret := tp.AddJobTimeout(int(common.RandInt()), LoadFileInfo{k, v}, 10)
				if ret {
					atomic.AddInt32(&loading, 1)
					break
				}
			}

			if time.Now().Sub(lastload) >= time.Second {
				lastload = time.Now()
				speed := float64(doneload) / float64(int(time.Now().Sub(beginload))/int(time.Second))
				left := ""
				if speed > 0 {
					left = time.Duration(int64(float64(dbtotal-int(doneload))/speed) * int64(time.Second)).String()
				}
				donesizem := doneloadsize / 1024 / 1024
				dataspeed := int(donesizem) / (int(time.Now().Sub(beginload)) / int(time.Second))
				loggo.Info("load speed=%.2f/s percent=%d%% time=%s thead=%d progress=%d/%d data=%dM dataspeed=%dM/s", speed, int(doneload)*100/dbtotal, left,
					loading, doneload, dbtotal, donesizem, dataspeed)
			}

			return nil
		})

		for loading != 0 {
			time.Sleep(time.Millisecond * 10)
		}

		tp.Stop()

		for _, k := range need_del {
			b.Delete([]byte(k))
		}

		return nil
	})

	loggo.Info("load_lib load database ok")

	loggo.Info("load_lib start get image file list")
	imagefilelist := make([]CalFileInfo, 0)
	cached := 0
	filepath.Walk(lib, func(path string, f os.FileInfo, err error) error {

		if f == nil || f.IsDir() {
			return nil
		}

		if !strings.HasSuffix(strings.ToLower(f.Name()), ".jpeg") &&
			!strings.HasSuffix(strings.ToLower(f.Name()), ".jpg") &&
			!strings.HasSuffix(strings.ToLower(f.Name()), ".png") &&
			!strings.HasSuffix(strings.ToLower(f.Name()), ".gif") {
			return nil
		}

		abspath, err := filepath.Abs(path)
		if err != nil {
			loggo.Error("load_lib get Abs fail %s %s %s", database, path, err)
			return nil
		}

		db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucket_name))
			v := b.Get([]byte(abspath))
			if v == nil {
				imagefilelist = append(imagefilelist, CalFileInfo{fi: FileInfo{abspath, 0, 0, 0, ""}})
			} else {
				cached++
			}
			return nil
		})

		return nil
	})

	loggo.Info("load_lib get image file list ok %d cache %d", len(imagefilelist), cached)

	loggo.Info("load_lib start calc image avg color %d", len(imagefilelist))
	var worker int32
	begin := time.Now()
	last := time.Now()
	var done int32
	var donesize int64

	atomic.AddInt32(&worker, 1)
	var save_inter int
	go save_to_database(&worker, &imagefilelist, db, &save_inter, bucket_name)

	scale := getScaler(scalealg)

	tp := threadpool.NewThreadPool(workernum, 16, func(in interface{}) {
		i := in.(int)
		calc_avg_color(&imagefilelist[i], &worker, &done, &donesize, scale, pixelsize)
	})

	i := 0
	for worker != 0 {
		if i < len(imagefilelist) {
			ret := tp.AddJobTimeout(int(common.RandInt()), i, 10)
			if ret {
				atomic.AddInt32(&worker, 1)
				i++
			}
		} else {
			time.Sleep(time.Millisecond * 10)
		}
		if time.Now().Sub(last) >= time.Second {
			last = time.Now()
			speed := float64(done) / float64(int(time.Now().Sub(begin))/int(time.Second))
			left := ""
			if speed > 0 {
				left = time.Duration(int64(float64(len(imagefilelist)-int(done))/speed) * int64(time.Second)).String()
			}
			donesizem := donesize / 1024 / 1024
			dataspeed := int(donesizem) / (int(time.Now().Sub(begin)) / int(time.Second))
			loggo.Info("calc speed=%.2f/s percent=%d%% time=%s thead=%d progress=%d/%d saved=%d data=%dM dataspeed=%dM/s", speed, int(done)*100/len(imagefilelist),
				left, int(worker), int(done), len(imagefilelist), save_inter, donesizem, dataspeed)
		}
	}
	tp.Stop()

	loggo.Info("load_lib calc image avg color ok %d %d", len(imagefilelist), done)

	loggo.Info("load_lib start save image avg color")

	maxcolornum := 0
	totalnum := 0
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket_name))

		b.ForEach(func(k, v []byte) error {

			var b bytes.Buffer
			b.Write(v)

			dec := gob.NewDecoder(&b)
			var fi FileInfo
			err = dec.Decode(&fi)
			if err != nil {
				loggo.Error("load_lib Open database Decode fail %s %s %s", database, string(k), err)
				return nil
			}

			key := make_key(fi.R, fi.G, fi.B)
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
	colorgourp := []struct {
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
			for index, cg := range colorgourp {
				diff := common.ColorDistance(color.RGBA{data.r, data.g, data.b, 0}, cg.c)
				if diff < mindistance {
					min = index
					mindistance = diff
				}
			}

			colorgourp[min].num += data.file
		}
	}

	for i := 0; i <= maxcolornum; i++ {
		str := ""
		if tmpcolornum[i] == 1 {
			str = make_string(tmpcolorone[i].r, tmpcolorone[i].g, tmpcolorone[i].b)
		}
		loggo.Info("load_lib avg color num distribution %d = %d %s", i, tmpcolornum[i], str)
	}

	maxcolorgroupnum := 0
	maxcolorgroupindex := 0
	for index, cg := range colorgourp {
		loggo.Info("load_lib avg color color distribution %s = %d", cg.name, cg.num)
		if cg.num > maxcolorgroupnum {
			maxcolorgroupnum = cg.num
			maxcolorgroupindex = index
		}
	}
	loggo.Info("load_lib avg color color max %s %d", colorgourp[maxcolorgroupindex].name, colorgourp[maxcolorgroupindex].num)

	return nil
}

func make_key(r uint8, g uint8, b uint8) int {
	return int(r)*256*256 + int(g)*256 + int(b)
}

func make_string(r uint8, g uint8, b uint8) string {
	return "r " + strconv.Itoa(int(r)) + " g " + strconv.Itoa(int(g)) + " b " + strconv.Itoa(int(b))
}

func calc_img(src image.Image, filename string, scaler draw.Scaler, pixelsize int) (image.Image, error) {

	bounds := src.Bounds()

	len := common.MinOfInt(bounds.Dx(), bounds.Dy())
	startx := bounds.Min.X + (bounds.Dx()-len)/2
	starty := bounds.Min.Y + (bounds.Dy()-len)/2
	endx := common.MinOfInt(startx+len, bounds.Max.X)
	endy := common.MinOfInt(starty+len, bounds.Max.Y)

	if startx != bounds.Min.X || starty != bounds.Min.Y || endx != bounds.Max.X || endy != bounds.Max.Y {
		dst := image.NewRGBA(image.Rectangle{image.Point{0, 0}, image.Point{len, len}})
		draw.Copy(dst, image.Point{0, 0}, src, image.Rectangle{image.Point{startx, starty}, image.Point{endx, endy}}, draw.Over, nil)
		src = dst
	}

	bounds = src.Bounds()
	if bounds.Dx() != bounds.Dy() {
		loggo.Error("calc_img cult image fail %s %d %d", filename, bounds.Dx(), bounds.Dy())
		return nil, errors.New("bounds error")
	}

	len = common.MinOfInt(bounds.Dx(), bounds.Dy())
	if len < pixelsize {
		loggo.Error("calc_img image too small %s %d %d", filename, len, pixelsize)
		return nil, errors.New("too small")
	}

	if len > pixelsize {
		rect := image.Rectangle{image.Point{0, 0}, image.Point{pixelsize, pixelsize}}
		dst := image.NewRGBA(rect)
		scaler.Scale(dst, rect, src, src.Bounds(), draw.Over, nil)
		src = dst
	}

	return src, nil
}

func calc_avg_color(cfi *CalFileInfo, worker *int32, done *int32, donesize *int64, scaler draw.Scaler, pixelsize int) {
	defer common.CrashLog()
	defer atomic.AddInt32(worker, -1)
	defer atomic.AddInt32(done, 1)
	defer func() {
		cfi.done = true
	}()

	reader, err := os.Open(cfi.fi.Filename)
	if err != nil {
		loggo.Error("calc_avg_color Open fail %s %s", cfi.fi.Filename, err)
		return
	}
	defer reader.Close()

	fi, err := reader.Stat()
	if err != nil {
		loggo.Error("calc_avg_color Stat fail %s %s", cfi.fi.Filename, err)
		return
	}
	filesize := fi.Size()
	defer atomic.AddInt64(donesize, filesize)

	img, _, err := image.Decode(reader)
	if err != nil {
		loggo.Error("calc_avg_color Decode image fail %s %s", cfi.fi.Filename, err)
		return
	}

	img, err = calc_img(img, cfi.fi.Filename, scaler, pixelsize)
	if err != nil {
		loggo.Error("calc_avg_color calc_img image fail %s %s", cfi.fi.Filename, err)
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

			count += 1
		}
	}

	readerhash, err := os.Open(cfi.fi.Filename)
	if err != nil {
		loggo.Error("calc_avg_color Open fail %s %s", cfi.fi.Filename, err)
		return
	}
	defer readerhash.Close()

	b, err := ioutil.ReadAll(readerhash)
	if err != nil {
		loggo.Error("calc_avg_color ReadAll fail %s %d", cfi.fi.Filename, pixelsize)
		return
	}

	cfi.fi.R = uint8(sumR / count)
	cfi.fi.G = uint8(sumG / count)
	cfi.fi.B = uint8(sumB / count)
	cfi.fi.Hash = common.GetXXHashString(string(b))
	cfi.ok = true

	return
}

func save_to_database(worker *int32, imagefilelist *[]CalFileInfo, db *bolt.DB, save_inter *int, bucket_name string) {
	defer common.CrashLog()
	defer atomic.AddInt32(worker, -1)

	i := 0
	for {
		if i >= len(*imagefilelist) {
			return
		}

		cfi := (*imagefilelist)[i]
		if cfi.done {
			i++

			if cfi.ok {
				var b bytes.Buffer

				enc := gob.NewEncoder(&b)
				err := enc.Encode(&cfi.fi)
				if err != nil {
					loggo.Error("calc_avg_color Encode FileInfo fail %s %s", cfi.fi.Filename, err)
					return
				}

				k := []byte(cfi.fi.Filename)
				v := b.Bytes()

				db.Update(func(tx *bolt.Tx) error {
					b := tx.Bucket([]byte(bucket_name))
					err := b.Put(k, v)
					return err
				})
			}

			*save_inter = i
		} else {
			time.Sleep(time.Millisecond * 10)
		}
	}
}

func gen_target(srcimg image.Image, target string, workernum int, database string, pixelsize int, maxsize int, scalealg string, libname string, cachemap *sync.Map) error {
	loggo.Info("gen_target %s", target)

	db, err := bolt.Open(database, 0600, nil)
	if err != nil {
		loggo.Error("gen_target Open database fail %s %s", database, err)
		return err
	}
	defer db.Close()

	bucket_name := "FileInfo" + libname + strconv.Itoa(pixelsize)

	bounds := srcimg.Bounds()

	startx := bounds.Min.X
	starty := bounds.Min.Y
	endx := bounds.Max.X
	endy := bounds.Max.Y

	last := time.Now()
	begin := time.Now()
	total := bounds.Dx() * bounds.Dy()
	var done int32
	var doing int32
	var cached int32

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

	tp := threadpool.NewThreadPool(workernum, 16, func(in interface{}) {
		defer atomic.AddInt32(&done, 1)
		defer atomic.AddInt32(&doing, -1)
		gi := in.(GenInfo)
		gen_target_pixel(gi.c, gi.x, gi.y, dst, db, bucket_name, pixelsize, scalealg, cachemap, &cached)
	})

	for y := starty; y < endy; y++ {
		for x := startx; x < endx; x++ {
			r, g, b, _ := srcimg.At(x, y).RGBA()
			r, g, b = r>>8, g>>8, b>>8

			for {
				ret := tp.AddJobTimeout(int(common.RandInt()), GenInfo{x: x, y: y, c: color.RGBA{uint8(r), uint8(g), uint8(b), 0}}, 10)
				if ret {
					atomic.AddInt32(&doing, 1)
					break
				}
			}

			if time.Now().Sub(last) >= time.Second {
				last = time.Now()
				speed := float64(done) / float64(int(time.Now().Sub(begin))/int(time.Second))
				left := ""
				if speed > 0 {
					left = time.Duration(int64(float64(total-int(done))/speed) * int64(time.Second)).String()
				}
				loggo.Info("gen speed=%.2f/s percent=%d%% time=%s thead=%d progress=%d/%d cached=%d cached-percent=%d%%", speed, int(done)*100/total,
					left, int(doing), int(done), total, cached, int(cached)*100/total)
			}
		}
	}

	for doing != 0 {
		time.Sleep(time.Millisecond * 10)
	}

	tp.Stop()

	loggo.Info("gen_target gen pixel ok %s", target)

	loggo.Info("gen_target start write file %s", target)
	dstFile, err := os.Create(target)
	if err != nil {
		loggo.Error("gen_target Create fail %s %s", target, err)
		return err
	}
	defer dstFile.Close()

	if strings.HasSuffix(strings.ToLower(target), ".png") {
		err = png.Encode(dstFile, dst)
	} else if strings.HasSuffix(strings.ToLower(target), ".jpg") {
		err = jpeg.Encode(dstFile, dst, &jpeg.Options{Quality: 100})
	}
	if err != nil {
		loggo.Error("gen_target Encode fail %s %s", target, err)
		return err
	}

	loggo.Info("gen_target write file ok %s", target)

	return nil
}

func gen_target_pixel(src color.RGBA, x int, y int, dst *image.RGBA, db *bolt.DB, bucket_name string, pixelsize int, scalealg string, cachemap *sync.Map, cached *int32) {

	var minimgs []image.Image

	key := make_string(src.R, src.G, src.B)
	v, ok := cachemap.Load(key)
	if ok {
		ci := v.(*CacheInfo)
		minimgs = ci.img
	}

	if len(minimgs) <= 0 {
		if ok {
			ci := v.(*CacheInfo)
			ci.lock.Lock()
		}

		if len(minimgs) <= 0 {

			mindiff := math.MaxFloat64
			var mindiffnames []string
			var minfi FileInfo

			db.View(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte(bucket_name))
				b.ForEach(func(k, v []byte) error {

					var b bytes.Buffer
					b.Write(v)

					dec := gob.NewDecoder(&b)
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

			for _, mindiffname := range mindiffnames {
				reader, err := os.Open(mindiffname)
				if err != nil {
					loggo.Error("gen_target_pixel Open fail %s %s", mindiffname, err)
					os.Exit(1)
				}
				defer reader.Close()

				minimg, _, err := image.Decode(reader)
				if err != nil {
					loggo.Error("gen_target_pixel Decode fail %s %s", mindiffname, err)
					return
				}

				scale := getScaler(scalealg)

				minimg, err = calc_img(minimg, mindiffname, scale, pixelsize)
				if err != nil {
					loggo.Error("gen_target_pixel calc_img image fail %s %s", mindiffname, err)
					return
				}

				minimgs = append(minimgs, minimg)
			}

			v, ok := cachemap.Load(key)
			if ok {
				ci := v.(*CacheInfo)
				ci.img = minimgs
			}
		} else {
			atomic.AddInt32(cached, 1)
		}

		if ok {
			ci := v.(*CacheInfo)
			ci.lock.Unlock()
		}
	} else {
		atomic.AddInt32(cached, 1)
	}

	var minimg image.Image
	minimg = minimgs[common.RandInt31n(len(minimgs))]

	if common.RandInt()%2 == 0 {
		flippedImg := image.NewRGBA(minimg.Bounds())
		for j := 0; j < minimg.Bounds().Dy(); j++ {
			for i := 0; i < minimg.Bounds().Dx(); i++ {
				flippedImg.Set((minimg.Bounds().Dx()-1)-i, j, minimg.At(i, j))
			}
		}
		minimg = flippedImg
	}

	draw.Copy(dst, image.Point{x * pixelsize, y * pixelsize}, minimg, minimg.Bounds(), draw.Over, nil)
}
