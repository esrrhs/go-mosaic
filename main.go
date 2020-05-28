package main

import (
	"bytes"
	"encoding/gob"
	"errors"
	"flag"
	"github.com/boltdb/bolt"
	"github.com/esrrhs/go-engine/src/common"
	"github.com/esrrhs/go-engine/src/loggo"
	"github.com/esrrhs/go-engine/src/threadpool"
	"golang.org/x/image/draw"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
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
	lib := flag.String("lib", "", "lib image path")
	worker := flag.Int("worker", 12, "worker thread num")
	database := flag.String("database", "./database.bin", "cache datbase")
	pixelsize := flag.Int("pixelsize", 256, "pic scale size per one pixel")
	scalealg := flag.String("scalealg", "CatmullRom", "pic scale function NearestNeighbor/ApproxBiLinear/BiLinear/CatmullRom")
	checkhash := flag.Bool("checkhash", true, "check database pic hash")

	flag.Parse()

	if *src == "" || *target == "" || *lib == "" {
		flag.Usage()
		return
	}
	if *scalealg != "NearestNeighbor" &&
		*scalealg != "ApproxBiLinear" &&
		*scalealg != "BiLinear" &&
		*scalealg != "CatmullRom" {
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

	err := parse_src(*src)
	if err != nil {
		return
	}
	err = load_lib(*lib, *worker, *database, *pixelsize, *scalealg, *checkhash)
	if err != nil {
		return
	}
	err = gen_target(*target)
	if err != nil {
		return
	}
}

func parse_src(src string) error {
	loggo.Info("parse_src %s", src)

	reader, err := os.Open(src)
	if err != nil {
		loggo.Error("parse_src Open fail %s %s", src, err)
		return err
	}
	defer reader.Close()

	fi, err := reader.Stat()
	if err != nil {
		loggo.Error("parse_src Stat fail %s %s", src, err)
		return err
	}
	filesize := fi.Size()

	img, _, err := image.Decode(reader)
	if err != nil {
		loggo.Error("parse_src Decode image fail %s %s", src, err)
		return err
	}

	bounds := img.Bounds()

	startx := bounds.Min.X
	starty := bounds.Min.Y
	endx := bounds.Max.X
	endy := bounds.Max.Y

	for y := starty; y < endy; y++ {
		for x := startx; x < endx; x++ {
			//r, g, b, _ := img.At(x, y).RGBA()

		}
	}

	loggo.Info("parse_src ok %s %d", src, filesize)
	return nil
}

func gen_target(target string) error {
	loggo.Info("gen_target %s", target)
	return nil
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

func load_lib(lib string, workernum int, database string, pixelsize int, scalealg string, checkhash bool) error {
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

	bucket_name := "FileInfo" + strconv.Itoa(pixelsize)

	dbtotal := 0
	db.View(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists([]byte(bucket_name))
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
				speed := int(doneload) / (int(time.Now().Sub(beginload)) / int(time.Second))
				left := ""
				if speed > 0 {
					left = time.Duration(int64((dbtotal-int(doneload))/speed) * int64(time.Second)).String()
				}
				donesizem := doneloadsize / 1024 / 1024
				dataspeed := int(donesizem) / (int(time.Now().Sub(beginload)) / int(time.Second))
				loggo.Info("load speed=%d/s percent=%d%% time=%s thead=%d progress=%d/%d data=%dM dataspeed=%dM/s", speed, int(doneload)*100/dbtotal, left,
					loading, doneload, dbtotal, donesizem, dataspeed)
			}

			return nil
		})

		for loading != 0 {
			time.Sleep(time.Millisecond * 10)
		}

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
			speed := int(done) / (int(time.Now().Sub(begin)) / int(time.Second))
			left := ""
			if speed > 0 {
				left = time.Duration(int64((len(imagefilelist)-int(done))/speed) * int64(time.Second)).String()
			}
			donesizem := donesize / 1024 / 1024
			dataspeed := int(donesizem) / (int(time.Now().Sub(begin)) / int(time.Second))
			loggo.Info("calc speed=%d/s percent=%d%% time=%s thead=%d progress=%d/%d saved=%d data=%dM dataspeed=%dM/s", speed, int(done)*100/len(imagefilelist),
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

	bounds := img.Bounds()

	len := common.MinOfInt(bounds.Dx(), bounds.Dy())
	startx := bounds.Min.X + (bounds.Dx()-len)/2
	starty := bounds.Min.Y + (bounds.Dy()-len)/2
	endx := common.MinOfInt(startx+len, bounds.Max.X)
	endy := common.MinOfInt(starty+len, bounds.Max.Y)

	if startx != bounds.Min.X || starty != bounds.Min.Y || endx != bounds.Max.X || endy != bounds.Max.Y {
		dst := image.NewRGBA(image.Rectangle{image.Point{0, 0}, image.Point{len, len}})
		draw.Copy(dst, image.Point{0, 0}, img, image.Rectangle{image.Point{startx, starty}, image.Point{endx, endy}}, draw.Over, nil)
		img = dst
	}

	bounds = img.Bounds()
	if bounds.Dx() != bounds.Dy() {
		loggo.Error("calc_avg_color cult image fail %s %d %d", cfi.fi.Filename, bounds.Dx(), bounds.Dy())
		return
	}

	len = common.MinOfInt(bounds.Dx(), bounds.Dy())
	if len < pixelsize {
		loggo.Error("calc_avg_color image to small %s %d %d", cfi.fi.Filename, len, pixelsize)
		return
	}

	if len > pixelsize {
		rect := image.Rectangle{image.Point{0, 0}, image.Point{pixelsize, pixelsize}}
		dst := image.NewRGBA(rect)
		scaler.Scale(dst, rect, img, img.Bounds(), draw.Over, nil)
		img = dst
	}

	bounds = img.Bounds()

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
		loggo.Error("calc_avg_color ReadAll fail %s %d %d", cfi.fi.Filename, len, pixelsize)
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
