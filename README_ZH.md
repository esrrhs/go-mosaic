# go-mosaic

[<img src="https://img.shields.io/github/license/esrrhs/go-mosaic">](https://github.com/esrrhs/go-mosaic)
[<img src="https://img.shields.io/github/languages/top/esrrhs/go-mosaic">](https://github.com/esrrhs/go-mosaic)
[<img src="https://img.shields.io/github/v/release/esrrhs/go-mosaic">](https://github.com/esrrhs/go-mosaic/releases)
[<img src="https://img.shields.io/github/downloads/esrrhs/go-mosaic/total">](https://github.com/esrrhs/go-mosaic/releases)
[<img src="https://img.shields.io/docker/pulls/esrrhs/go-mosaic">](https://hub.docker.com/repository/docker/esrrhs/go-mosaic)
[<img src="https://img.shields.io/github/actions/workflow/status/esrrhs/go-mosaic/go.yml?branch=master">](https://github.com/esrrhs/go-mosaic/actions)
[![Go Version](https://img.shields.io/github/go-mod/go-version/esrrhs/go-mosaic)](https://golang.org)

go-mosaic 是一个高效制作相片马赛克（蒙太奇拼贴照片）的命令行工具。利用图像处理算法，将海量素材小图通过颜色匹配与几何缩放，拼贴成一张宏观与细节兼备的艺术马赛克大图。

[English](./README.md)

## 特性

* **高性能与高并发**：使用多协程并发计算与图片缩放，全面支持多核 CPU。
* **内建嵌入式缓存**：采用现代化 bbolt 存储图片特征色彩与哈希，支持断点恢复；库中图片增删自动感知与缓存同步。
* **现代化 Go 技术栈**：基于 Go 1.22+，全并发无竞态安全（通过 `-race` 检查），提供标准单元测试与端到端集成测试。
* **多平台支持**：提供 Linux、macOS（含 Apple Silicon M系列）、Windows 等全平台交叉编译与 Docker 容器化支持。

## 快速上手

### 1. 编译或下载

* 从 [GitHub Releases](https://github.com/esrrhs/go-mosaic/releases) 下载适合您操作系统的预编译包。
* 或者直接使用 Go 本地构建：
  ```bash
  git clone https://github.com/esrrhs/go-mosaic.git
  cd go-mosaic
  make build
  ```

### 2. 执行拼贴

```bash
./go-mosaic -src input.png -target output.jpg -lib ./test
```

* `-src`：目标轮廓图（最终马赛克拼图的主题原图）。
* `-target`：输出马赛克大图路径（支持 `.png`、`.jpg`、`.jpeg`）。
* `-lib`：素材图库文件夹（组成马赛克微元的小图库，素材越丰富，拼图色彩与逼真度越高）。

### 3. Docker 使用

```bash
# 构建镜像
docker build -t go-mosaic:latest .

# 运行容器生成马赛克
docker run --rm -v $(pwd):/workspace go-mosaic:latest \
  -src /workspace/input.png \
  -target /workspace/output.png \
  -lib /workspace/test
```

## 参数说明

```text
Usage of go-mosaic:
  -checkhash
    	check database pic hash (default true)
  -database string
    	cache database (default "./database.bin")
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

## 开发与测试

```bash
# 运行单元测试与竞态检测
make test

# 代码静态检查
make vet

# 跨平台构建打包
make pack

# 清理构建产物
make clean
```

## 效果示例

| 原始目标图 | 马赛克生成大图 |
| :---: | :---: |
| ![input](input.png) | ![output](smalloutput.png) |
