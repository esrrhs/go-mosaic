package main

import (
	"bytes"
	"encoding/gob"
	"flag"
	"github.com/boltdb/bolt"
	"github.com/esrrhs/go-engine/src/common"
	"github.com/esrrhs/go-engine/src/loggo"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

func main() {

	defer common.CrashLog()

	src := flag.String("src", "", "src image path")
	target := flag.String("target", "", "target image path")
	lib := flag.String("lib", "", "lib image path")
	worker := flag.Int("worker", 8, "worker thread num")
	database := flag.String("cache", "./database.bin", "cache datbase")

	flag.Parse()

	if *src == "" || *target == "" || *lib == "" {
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

	parse_src(*src)
	load_lib(*lib, *worker, *database)
	gen_target(*target)
}

func parse_src(src string) {
	loggo.Info("parse_src %s", src)

}

func gen_target(target string) {
	loggo.Info("gen_target %s", target)

}

type FileInfo struct {
	Filename string
	R        uint8
	G        uint8
	B        uint8
}

type ImageData struct {
	filename []string
	index    int
	r        uint8
	g        uint8
	b        uint8
}

var gcolordata []ImageData

func load_lib(lib string, workernum int, database string) {
	loggo.Info("load_lib %s", lib)

	loggo.Info("load_lib start ini database")
	for i := 0; i <= 255; i++ {
		for j := 0; j <= 255; j++ {
			for z := 0; z <= 255; z++ {
				gcolordata = append(gcolordata, ImageData{})
			}
		}
	}

	for i := 0; i <= 255; i++ {
		for j := 0; j <= 255; j++ {
			for z := 0; z <= 255; z++ {
				k := make_key(uint8(i), uint8(j), uint8(z))
				gcolordata[k].r, gcolordata[k].g, gcolordata[k].b = uint8(i), uint8(j), uint8(z)
			}
		}
	}

	loggo.Info("load_lib ini database ok")

	loggo.Info("load_lib start load database")

	db, err := bolt.Open(database, 0600, nil)
	if err != nil {
		loggo.Error("load_lib Open database fail %s %s", database, err)
	}
	defer db.Close()

	db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists([]byte("FileInfo"))
		b := tx.Bucket([]byte("FileInfo"))

		need_del := make([]string, 0)

		b.ForEach(func(k, v []byte) error {

			var b bytes.Buffer
			b.Write(v)

			dec := gob.NewDecoder(&b)
			var fi FileInfo
			err = dec.Decode(&fi)
			if err != nil {
				loggo.Error("load_lib Open database Decode fail %s %s %s", database, string(k), err)
				need_del = append(need_del, string(k))
				return nil
			}

			if _, err := os.Stat(fi.Filename); os.IsNotExist(err) {
				loggo.Error("load_lib Open Filename IsNotExist, need delete %s %s %s", database, fi.Filename, err)
				need_del = append(need_del, string(k))
				return nil
			}

			return nil
		})

		for _, k := range need_del {
			b.Delete([]byte(k))
		}

		return nil
	})

	loggo.Info("load_lib load database ok")

	loggo.Info("load_lib start get image file list")
	imagefilelist := make([]FileInfo, 0)
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

		db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("FileInfo"))
			v := b.Get([]byte(path))
			if v == nil {
				imagefilelist = append(imagefilelist, FileInfo{path, 0, 0, 0})
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
	for _, fi := range imagefilelist {
		if worker > int32(workernum) {
			atomic.AddInt32(&worker, 1)
			calc_avg_color(fi, &worker, &done, db)
		} else {
			atomic.AddInt32(&worker, 1)
			go calc_avg_color(fi, &worker, &done, db)
		}
		if time.Now().Sub(last) >= time.Second {
			last = time.Now()
			speed := int(done) / (int(time.Now().Sub(begin)) / int(time.Second))
			loggo.Info("load_lib calc image avg color speed %d/s %d%%", speed, int(done)*100/len(imagefilelist))
		}
	}

	for {
		if worker == 0 {
			time.Sleep(time.Second)
			break
		}
		time.Sleep(time.Second)
	}

	loggo.Info("load_lib calc image avg color ok %d %d", len(imagefilelist), done)

	loggo.Info("load_lib start save image avg color")

	maxcolornum := 0
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("FileInfo"))

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
			gcolordata[key].filename = append(gcolordata[key].filename, fi.Filename)
			if len(gcolordata[key].filename) > maxcolornum {
				maxcolornum = len(gcolordata[key].filename)
			}

			return nil
		})

		return nil
	})

	loggo.Info("load_lib save image avg color ok max %d", maxcolornum)
}

func make_key(r uint8, g uint8, b uint8) int {
	return int(r)*256*256 + int(g)*256 + int(b)
}

func calc_avg_color(fi FileInfo, worker *int32, done *int32, db *bolt.DB) {
	defer common.CrashLog()
	defer atomic.AddInt32(worker, -1)
	defer atomic.AddInt32(done, 1)

	reader, err := os.Open(fi.Filename)
	if err != nil {
		loggo.Error("calc_avg_color Open fail %s %s", fi.Filename, err)
		return
	}
	defer reader.Close()

	img, _, err := image.Decode(reader)
	if err != nil {
		loggo.Error("calc_avg_color Decode image fail %s %s", fi.Filename, err)
		return
	}

	bounds := img.Bounds()

	var sumR, sumG, sumB, count float64

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()

			sumR += float64(r)
			sumG += float64(g)
			sumB += float64(b)

			count += 1
		}
	}

	fi.R = uint8(sumR / count)
	fi.G = uint8(sumG / count)
	fi.B = uint8(sumB / count)

	var b bytes.Buffer

	enc := gob.NewEncoder(&b)
	err = enc.Encode(&fi)
	if err != nil {
		loggo.Error("calc_avg_color Encode FileInfo fail %s %s", fi.Filename, err)
		return
	}

	k := []byte(fi.Filename)
	v := b.Bytes()

	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("FileInfo"))
		err := b.Put(k, v)
		return err
	})

	return
}
