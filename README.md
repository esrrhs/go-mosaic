# go-mosaic
go-mosaic是一个制作相片馬賽克的工具。相片馬賽克，或稱蒙太奇照片、蒙太奇拼貼，是一種影像處理的藝術技巧，利用這個方式做出來的圖片，近看時是由許多張小照片合在一起的，但遠看時，每張照片透過光影和色彩的微調，組成了一張大圖的基本像素，就叫做相片馬賽克技巧。

# 特性
* 内建数据库优化海量图片使用
* 多核构建提升生成速度

# 使用
* 准备好一个图片文件夹，用来组成最终图片的元素，假设为./pic
* 准备好一张目标图片，用来生成在最终的大图，假设为input.jpg，生成的大图为output.jpg
* 输入命令，等待完成
```
go-mosaic.exe -src input.jpg -target output.jpg -lib ./pic
```
* 更多参数，参考help
```
Usage of D:\project\go-mosaic\aa.exe:
  -checkhash
    	check database pic hash (default true)
  -database string
    	cache datbase (default "./database.bin")
  -lib string
    	lib image path
  -maxsize int
    	pic max size in GB (default 4)
  -pixelsize int
    	pic scale size per one pixel (default 256)
  -scalealg string
    	pic scale function NearestNeighbor/ApproxBiLinear/BiLinear/CatmullRom (default "CatmullRom")
  -src string
    	src image path
  -target string
    	target image path
  -worker int
    	worker thread num (default 12)
```

# 示例


