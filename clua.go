package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type FileData struct {
	file string
	path string
	line map[int]uint64
}

func main() {

	input := flag.String("i", "", "input cov file")
	root := flag.String("path", "./", "source code path")
	filter := flag.String("f", "", "filter filename")

	flag.Parse()

	if len(*input) == 0 {
		flag.Usage()
		return
	}

	filename := *input
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("ReadFile fail %v\n", err)
		return
	}

	var filedata []FileData
	n := 0
	i := 0
	for {
		if i+4 >= len(data) {
			break
		}
		strlen := binary.LittleEndian.Uint32(data[i : i+4])
		i += 4

		if i+int(strlen) >= len(data) {
			break
		}
		str := string(data[i : i+int(strlen)])
		i += int(strlen)
		if i >= len(data) {
			break
		}

		if i+8 >= len(data) {
			break
		}
		count := binary.LittleEndian.Uint64(data[i : i+8])
		i += 8

		str = strings.TrimLeft(str, "@")
		if strings.Count(str, ":") != 1 {
			continue
		}
		params := strings.Split(str, ":")
		if len(params) < 2 {
			fmt.Printf("Split fail %s\n", str)
			return
		}
		filename := params[0]
		line, err := strconv.Atoi(params[1])
		if err != nil {
			fmt.Printf("Atoi fail  %s %v\n", str, err)
			return
		}

		path, err := filepath.Abs(*root + "/" + filename)
		if err != nil {
			fmt.Printf("Path fail %s %s %v\n", *root, str, err)
			return
		}

		if !fileExists(path) {
			fmt.Printf("File not found %s\n", path)
			return
		}

		file := filepath.Base(path)
		file = strings.TrimSuffix(file, filepath.Ext(file))

		find := false
		for index, _ := range filedata {
			if filedata[index].path == path {
				filedata[index].line[line] += count
				find = true
				break
			}
		}

		if !find {
			f := FileData{file, path, make(map[int]uint64)}
			f.line[line] = count
			filedata = append(filedata, f)
		}

		n++
	}

	fmt.Printf("total points = %d, files = %d\n", n, len(filedata))

	if len(*filter) != 0 {
		for _, p := range filedata {
			if p.file == *filter {
				calc(p)
			}
		}
	} else {
		for _, p := range filedata {
			calc(p)
		}
	}
}

func calc(f FileData) {

	fmt.Printf("coverage of %s:\n", f.path)

	file, err := os.Open(f.path)
	defer file.Close()

	if err != nil {
		fmt.Printf("Open File Fail %v\n", err)
		return
	}

	maxpre := uint64(0)
	for _, c := range f.line {
		if c > maxpre {
			maxpre = c
		}
	}
	pre := 0
	for maxpre > 0 {
		maxpre /= 10
		pre++
	}

	// Start reading from the file with a reader.
	reader := bufio.NewReader(file)

	n := 1
	valid := 0
	for {
		str, err := reader.ReadString('\n')
		val, ok := f.line[n]
		if ok {
			fmt.Printf(fmt.Sprintf("%%-%d", pre)+"v", val)
		} else {
			fmt.Printf(fmt.Sprintf("%%-%d", pre)+"v", " ")
		}
		fmt.Printf(" %s\n", strings.TrimRight(str, "\n"))

		str = strings.TrimSpace(str)
		if str == "" || str == "end" || str == "else" ||
			strings.HasPrefix(str, "--") {
		} else {
			valid++
		}

		n++
		if err != nil {
			break
		}
	}

	fmt.Printf("%s total coverage %d%%\n", f.path, len(f.line)*100/valid)
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
