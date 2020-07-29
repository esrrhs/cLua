package main

import (
	"bufio"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/milochristiansen/lua/ast"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type FileData struct {
	file    string
	path    string
	line    map[int]uint64
	srcfile string
}

type arrayFlags []string

func (f *arrayFlags) String() string {
	return ""
}

func (f *arrayFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {

	var inputs arrayFlags
	flag.Var(&inputs, "i", "input cov file")
	root := flag.String("path", "./", "source code path")
	filter := flag.String("f", "", "filter filename")
	filterpath := flag.String("fp", "", "filter filepath")
	showcode := flag.Bool("showcode", true, "show code")
	showtotal := flag.Bool("showtotal", true, "show total")
	showfunc := flag.Bool("showfunc", true, "show func")
	showfile := flag.Bool("showfile", false, "show file")
	lcovfile := flag.String("lcov", "", "output lcov info")
	mergeto := flag.String("o", "dst.cov", "merge dst")
	filterpathsource := flag.String("fpsource", "", "when filter filepath, use the special source file path")
	lcovmd5 := flag.Bool("lcovmd5", false, "output lcov info with md5")
	reverse := flag.Bool("reverse", false, "reverse from lcov file to cov file")

	flag.Parse()

	if len(inputs) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	if *reverse {
		reverse_to_cov(*root, *lcovfile, inputs[0])
		return
	}

	var filedatas [][]FileData

	for _, input := range inputs {
		filedata, ok := parse(input, *root)
		if !ok {
			os.Exit(1)
		}
		filedatas = append(filedatas, filedata)
	}

	if *showfile {
		for _, filedata := range filedatas {
			for _, p := range filedata {
				fmt.Println(filepath.Clean(p.path))
			}
		}
	}

	if *showcode || *showtotal || *showfunc || len(*lcovfile) != 0 {
		err, lcovfd := check_lcovfile_begin(*lcovfile)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if len(*filter) != 0 || len(*filterpath) != 0 {
			for _, filedata := range filedatas {
				for _, p := range filedata {
					if p.file == *filter || filepath.Clean(p.path) == filepath.Clean(*filterpath) {
						if len(*filterpath) != 0 && len(*filterpathsource) != 0 {
							p.path = *filterpathsource
						}
						calc(p, *showcode, *showtotal, *showfunc, lcovfd, *lcovmd5)
					}
				}
			}
		} else {
			for _, filedata := range filedatas {
				for _, p := range filedata {
					calc(p, *showcode, *showtotal, *showfunc, lcovfd, *lcovmd5)
				}
			}
		}

		check_lcovfile_end(lcovfd)
	}

	if *mergeto != "" {
		merge(filedatas, *mergeto)
	}
}

func reverse_to_cov(root string, lcovfile string, covfile string) {

	data, err := ioutil.ReadFile(lcovfile)
	if err != nil {
		fmt.Printf("ReadFile fail %v\n", err)
		os.Exit(1)
	}

	absroot, err := filepath.Abs(root)
	if err != nil {
		fmt.Printf("filepath Abs fail %v\n", err)
		os.Exit(1)
	}

	tmpout := make(map[string]uint64)

	filename := ""
	linedata := make(map[int]uint64)

	datastr := string(data)
	for _, str := range strings.Split(datastr, "\n") {
		if strings.HasPrefix(str, "SF:") {
			str = strings.TrimLeft(str, "SF:")
			str = strings.TrimSpace(str)
			filename, err = filepath.Rel(absroot, str)
			if err != nil {
				fmt.Printf("filepath Rel fail %v\n", err)
				os.Exit(1)
			}
		} else if strings.HasPrefix(str, "DA:") {
			str = strings.TrimLeft(str, "DA:")
			str = strings.TrimSpace(str)
			params := strings.Split(str, ",")
			if len(params) < 2 {
				fmt.Printf("parse DA fail %s\n", str)
				os.Exit(1)
			}
			line, err := strconv.Atoi(params[0])
			if err != nil {
				fmt.Printf("Atoi fail %v\n", err)
				os.Exit(1)
			}
			data, err := strconv.Atoi(params[1])
			if err != nil {
				fmt.Printf("Atoi fail %v\n", err)
				os.Exit(1)
			}
			linedata[line] += uint64(data)
		} else if strings.HasPrefix(str, "end_of_record") {
			for line, n := range linedata {
				str := filename + ":" + strconv.Itoa(line)
				tmpout[str] += n
			}
		}
	}

	output_covfile(covfile, tmpout)
}

func merge(filedatas [][]FileData, dstfile string) {

	tmp := make(map[string]FileData)

	for _, filedata := range filedatas {
		for _, p := range filedata {
			path := p.path
			_, ok := tmp[path]
			if !ok {
				tmp[path] = p
			} else {
				for k, v := range p.line {
					tmp[path].line[k] += v
				}
			}
		}
	}

	tmpout := make(map[string]uint64)
	for _, filedata := range tmp {
		for k, v := range filedata.line {
			str := filedata.srcfile + ":" + strconv.Itoa(k)
			tmpout[str] += v
		}
	}

	output_covfile(dstfile, tmpout)
}

func output_covfile(dstfile string, tmpout map[string]uint64) {

	f, err := os.Create(dstfile)
	if err != nil {
		fmt.Printf("Create fail %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	for k, v := range tmpout {
		strlen := len(k)
		var lenbuf [4]byte
		binary.LittleEndian.PutUint32(lenbuf[:], uint32(strlen))
		f.Write(lenbuf[:])

		f.Write([]byte(k))

		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		f.Write(buf[:])
	}

}

func parse(filename string, root string) ([]FileData, bool) {

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("ReadFile fail %v\n", err)
		return nil, false
	}

	var filedata []FileData
	n := 0
	i := 0
	for {
		if i+4 > len(data) {
			break
		}
		strlen := binary.LittleEndian.Uint32(data[i : i+4])
		i += 4

		if i+int(strlen) > len(data) {
			break
		}
		str := string(data[i : i+int(strlen)])
		i += int(strlen)
		if i >= len(data) {
			break
		}

		if i+8 > len(data) {
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
			return nil, false
		}
		filename := params[0]
		line, err := strconv.Atoi(params[1])
		if err != nil {
			fmt.Printf("Atoi fail  %s %v\n", str, err)
			return nil, false
		}

		path, err := filepath.Abs(root + "/" + filename)
		if err != nil {
			fmt.Printf("Path fail %s %s %v\n", root, str, err)
			return nil, false
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
			f := FileData{file, path, make(map[int]uint64), filename}
			f.line[line] = count
			filedata = append(filedata, f)
		}

		n++
	}

	//fmt.Printf("total points = %d, files = %d\n", n, len(filedata))

	return filedata, true
}

func readfile(filename string) ([]string, bool) {

	var filecontent []string

	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Open File Fail %v\n", err)
		return filecontent, false
	}
	defer file.Close()

	// Start reading from the file with a reader.
	reader := bufio.NewReader(file)

	for {
		str, err := reader.ReadString('\n')
		filecontent = append(filecontent, str)
		if err != nil {
			break
		}
	}

	return filecontent, true
}

type luaVisitor struct {
	f func(n ast.Node)
}

func (lv *luaVisitor) Visit(n ast.Node) ast.Visitor {
	if n != nil {
		lv.f(n)
		return lv
	} else {
		return nil
	}
}

func do_showcode(f FileData, filecontent []string) {
	fmt.Printf("coverage of %s:\n", f.path)

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

	for index, str := range filecontent {
		val, ok := f.line[index+1]
		if ok {
			fmt.Printf(fmt.Sprintf("%%-%d", pre)+"v", val)
		} else {
			fmt.Printf(fmt.Sprintf("%%-%d", pre)+"v", " ")
		}
		fmt.Printf(" %s\n", strings.TrimRight(str, "\n"))
	}
}

func do_showtotal(f FileData, filecontent []string, validline map[int]int) {
	valid := 0
	for index, _ := range filecontent {
		_, ok := f.line[index+1]
		if ok {
			_, ok = validline[index+1]
			if ok {
				valid++
			}
		}
	}
	if len(validline) != 0 {
		fmt.Printf("%s total coverage %d%% %d/%d\n", f.path, valid*100/len(validline), valid, len(validline))
	} else {
		fmt.Printf("%s total coverage %d%% %d/%d\n", f.path, 0, valid, 0)
	}
}

func do_showfunc(f FileData, filecontent []string, block []ast.Stmt) {
	var funcdecs []*ast.FuncDecl
	v := luaVisitor{f: func(n ast.Node) {
		if n != nil {
			switch nn := n.(type) {
			case *ast.FuncDecl:
				funcdecs = append(funcdecs, nn)
			}
		}
	}}
	for _, stmt := range block {
		ast.Walk(&v, stmt)
	}

	for _, funcdec := range funcdecs {
		line := funcdec.Line()
		funcname := filecontent[line-1]
		funcname = strings.TrimSpace(funcname)
		funcname = "[" + funcname + "]"

		funcmaxline := 0
		funcvalidline := make(map[int]int)
		fv := luaVisitor{f: func(n ast.Node) {
			funcvalidline[n.Line()]++
			if n.Line() > funcmaxline {
				funcmaxline = n.Line()
			}
		}}
		for _, stmt := range funcdec.Block {
			ast.Walk(&fv, stmt)
		}

		valid := 0
		for i := line; i <= funcmaxline; i++ {
			_, ok := f.line[i]
			if ok {
				_, ok = funcvalidline[i]
				if ok {
					valid++
				}
			}
		}

		if len(funcvalidline) != 0 {
			fmt.Printf("%s function coverage %s %d%% %d/%d\n", f.path, funcname, valid*100/len(funcvalidline), valid, len(funcvalidline))
		} else {
			fmt.Printf("%s function coverage %s %d%% %d/%d\n", f.path, funcname, 0, valid, 0)
		}
	}
}

func do_lcovfile(f FileData, filecontent []string, block []ast.Stmt, validline map[int]int, lcovfd *os.File, lcovmd5 bool) {

	lcovfd.WriteString(fmt.Sprintf("SF:%s\n", f.path))

	var funcdecs []*ast.FuncDecl
	v := luaVisitor{f: func(n ast.Node) {
		if n != nil {
			switch nn := n.(type) {
			case *ast.FuncDecl:
				funcdecs = append(funcdecs, nn)
			}
		}
	}}
	for _, stmt := range block {
		ast.Walk(&v, stmt)
	}

	for _, funcdec := range funcdecs {
		line := funcdec.Line()
		funcname := filecontent[line-1]
		funcname = strings.TrimSpace(funcname)
		funcname = "[" + funcname + "]"

		lcovfd.WriteString(fmt.Sprintf("FN:%d,%s\n", line, funcname))
	}

	funcfound := 0
	funchit := 0
	for _, funcdec := range funcdecs {
		line := funcdec.Line()

		funcname := filecontent[line-1]
		funcname = strings.TrimSpace(funcname)
		funcname = "[" + funcname + "]"

		funcmaxline := 0
		funcvalidline := make(map[int]int)
		fv := luaVisitor{f: func(n ast.Node) {
			funcvalidline[n.Line()]++
			if n.Line() > funcmaxline {
				funcmaxline = n.Line()
			}
		}}
		for _, stmt := range funcdec.Block {
			ast.Walk(&fv, stmt)
		}

		var total_valid uint64
		for i := line; i <= funcmaxline; i++ {
			value, ok := f.line[i]
			if ok {
				_, ok = funcvalidline[i]
				if ok {
					total_valid += value
				}
			}
		}

		if len(funcvalidline) != 0 {
			lcovfd.WriteString(fmt.Sprintf("FNDA:%d,%s\n", total_valid, funcname))
		} else {
			lcovfd.WriteString(fmt.Sprintf("FNDA:%d,%s\n", 0, funcname))
		}

		funcfound++
		if total_valid > 0 {
			funchit++
		}
	}

	lcovfd.WriteString(fmt.Sprintf("FNF:%d\n", funcfound))
	lcovfd.WriteString(fmt.Sprintf("FNH:%d\n", funchit))

	for _, funcdec := range funcdecs {
		line := funcdec.Line()

		funcmaxline := 0
		funcvalidline := make(map[int]int)
		fv := luaVisitor{f: func(n ast.Node) {
			funcvalidline[n.Line()]++
			if n.Line() > funcmaxline {
				funcmaxline = n.Line()
			}
		}}
		for _, stmt := range funcdec.Block {
			ast.Walk(&fv, stmt)
		}

		for i := line; i <= funcmaxline; i++ {
			_, ok := funcvalidline[i]
			if ok {
				value, _ := f.line[i]
				if lcovmd5 {
					srcstr := filecontent[i-1]
					srcstr = strings.TrimRight(srcstr, "\r\n")
					srcstr = strings.TrimRight(srcstr, "\n")
					h := md5.New()
					h.Write([]byte(srcstr))
					md5str := base64.URLEncoding.EncodeToString(h.Sum(nil))
					md5str = strings.TrimRight(md5str, "==")
					md5str = strings.Replace(md5str, "_", "/", -1)
					md5str = strings.Replace(md5str, "-", "+", -1)
					lcovfd.WriteString(fmt.Sprintf("DA:%d,%d,%s\n", i, value, md5str))
				} else {
					lcovfd.WriteString(fmt.Sprintf("DA:%d,%d\n", i, value))
				}
			}
		}
	}

	linehit := 0
	for index, _ := range filecontent {
		_, ok := f.line[index+1]
		if ok {
			_, ok = validline[index+1]
			if ok {
				linehit++
			}
		}
	}

	lcovfd.WriteString(fmt.Sprintf("LF:%d\n", len(validline)))
	lcovfd.WriteString(fmt.Sprintf("LH:%d\n", linehit))

	lcovfd.WriteString("end_of_record\n")
}

func check_lcovfile_begin(lcovfile string) (error, *os.File) {
	if lcovfile != "" {
		file, err := os.OpenFile(lcovfile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
		if err != nil {
			return err, nil
		}
		file.WriteString("TN:\n")
		return nil, file
	}
	return nil, nil
}

func check_lcovfile_end(lcovfd *os.File) {
	if lcovfd != nil {
		lcovfd.Close()
	}
}

func calc(f FileData, showcode bool, showtotal bool, showfunc bool, lcovfd *os.File, lcovmd5 bool) {

	filecontent, ok := readfile(f.path)
	if !ok {
		os.Exit(1)
	}

	block, ok := parseLua(filecontent)
	if !ok {
		os.Exit(1)
	}

	validline := make(map[int]int)
	v := luaVisitor{f: func(n ast.Node) {
		validline[n.Line()]++
	}}
	for _, stmt := range block {
		ast.Walk(&v, stmt)
	}

	if showcode {
		do_showcode(f, filecontent)
	}

	if showtotal {
		do_showtotal(f, filecontent, validline)
	}

	if showfunc {
		do_showfunc(f, filecontent, block)
	}

	if lcovfd != nil {
		do_lcovfile(f, filecontent, block, validline, lcovfd, lcovmd5)
	}
}

func parseLua(filecontent []string) ([]ast.Stmt, bool) {

	source := ""
	for _, str := range filecontent {
		if strings.HasPrefix(str, "#!") {
			str = "\n"
		}
		source += str
	}

	block, err := ast.Parse(string(source), 1)
	if err != nil {
		fmt.Printf("Parse File Fail %v\n", err)
		return nil, false
	}

	return block, true
}
