# go-mosaic
go-mosaic是一个制作相片马赛克的工具。

# 特性
* 专为海量图片设计，可支持数万张图片
* 内建缓存数据库，图片删除、更改自动从缓存剔除
* 多核构建，加载、计算、替换均为并发

# 使用
* 克隆项目，编译，或者下载release
* 执行命令，等待完成
```
go-mosaic.exe -src input.png -target output.jpg -lib ./test
```
* 其中./test为图片文件夹，用来组成最终图片的元素。input.png为目标图片，用来生成最终的大图output.jpg
* 更多参数，参考help
```
Usage of D:\project\go-mosaic\test.exe:
  -checkhash
    	check database pic hash (default true)
  -database string
    	cache datbase (default "./database.bin")
  -lib string
    	image lib path
  -libname string
    	image lib name in database (default "default")
  -maxsize int
    	pic max size in GB (default 4)
  -pixelsize int
    	pic scale size per one pixel (default 64)
  -scalealg string
    	pic scale function NearestNeighbor/ApproxBiLinear/BiLinear/CatmullRom (default "CatmullRom")
  -src string
    	src image path
  -srcsize int
    	src image auto scale pixel size (default 128)
  -target string
    	target image path
  -worker int
    	worker thread num (default 12)
```

# 示例
![image](input.png)
![image](output.png)


