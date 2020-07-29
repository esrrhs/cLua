package main

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"flag"
	"github.com/esrrhs/go-engine/src/common"
	"github.com/esrrhs/go-engine/src/loggo"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var ty = flag.String("type", "client", "client / server / gen")
var root = flag.String("path", "./", "source code path")
var skiproot = flag.String("skippath", "tables", "skip path")
var binname = flag.String("bin", "main", "binary name")
var hookso = flag.String("hookso", "./hookso", "hookso path")
var libclua = flag.String("libclua", "./libclua.so", "libclua.so path")
var clua = flag.String("clua", "./clua", "clua path")
var covpath = flag.String("covpath", "./cov", "saved coverage path")
var covinter = flag.Int("covinter", 5, "saved coverage inter")
var server = flag.String("server", "http://127.0.0.1:8877", "send to server host")
var port = flag.Int("port", 8877, "server listen port")
var getluastate = flag.String("getluastate", "test.so lua_settop 1", "get lua state command")
var tmppath = flag.String("tmppath", "./tmp", "tmp path")
var lcov = flag.String("lcov", "lcov", "lcov bin path")
var paralel = flag.Int("paralel", 8, "max paralel")
var clientroot = flag.String("clientpath", "./", "client source code path")
var genhtml = flag.String("genhtml", "genhtml", "genhtml bin path")
var htmloutputpath = flag.String("htmlout", "./htmlout", "html output path")

func main() {

	flag.Parse()

	loggo.Ini(loggo.Config{
		Level:      loggo.LEVEL_INFO,
		Prefix:     "cluahelper",
		MaxDay:     3,
		NoLogFile:  false,
		NoPrint:    false,
		NoLogColor: false,
	})

	if *ty == "client" {
		ini_client()
	} else if *ty == "server" {
		ini_server()
	} else if *ty == "gen" {
		ini_gen()
	}

}

/////////////////////////////////////////////////////////////////////////////////

type SouceData struct {
	Content string
	Md5sum  string
	Id      string
}

type PushData struct {
	Covdata [][]byte
	Source  map[string]SouceData
}

/////////////////////////////////////////////////////////////////////////////////

func load_pids() ([]int, error) {
	var pids []int
	cmd := exec.Command("bash", "-c", "ps -ef | grep \""+*binname+" \" | grep -v grep | grep -v clua_helper | awk '{print $2}' ")
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s", err)
		return pids, err
	}
	//loggo.Info("pids = %s", string(out))
	pidstrs := strings.Split(string(out), "\n")
	for _, pidstr := range pidstrs {
		pidstr = strings.TrimSpace(pidstr)
		pid, err := strconv.Atoi(pidstr)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func get_lstate(pid int) (string, error) {

	// ./hookso arg $PID test.so lua_settop 1
	cmd := exec.Command("bash", "-c", *hookso+" arg "+strconv.Itoa(pid)+" "+*getluastate)
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s", err)
		return "", err
	}
	lstatestr := string(out)
	lstatestr = strings.TrimSpace(lstatestr)
	loggo.Info("pid %d L = %s", pid, lstatestr)

	// ./hookso dlopen $PID ./libclua.so
	cmd = exec.Command("bash", "-c", *hookso+" dlopen "+strconv.Itoa(pid)+" "+*libclua)
	out, err = cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s", err)
		return "", err
	}

	return lstatestr, nil
}

func stop_inject(pid int) error {

	loggo.Info("start stop_inject %d", pid)

	lstatestr, err := get_lstate(pid)
	if err != nil {
		loggo.Error("get_lstate failed with %s", err)
		return err
	}

	// ./hookso call $PID libclua.so stop_cov i=$L
	cmd := exec.Command("bash", "-c", *hookso+" call "+strconv.Itoa(pid)+" "+*libclua+" stop_cov i="+lstatestr)
	_, err = cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s", err)
		return err
	}

	loggo.Info("end stop_inject %d", pid)
	return nil
}

func get_pid_cov_file(pid int) (string, error) {

	thecovpath, err := filepath.Abs(*covpath)
	if err != nil {
		loggo.Error("filepath Abs failed with %s", err)
		return "", err
	}

	err = os.MkdirAll(thecovpath, 0755)
	if err != nil {
		loggo.Error("os MkdirAll failed with %s", err)
		return "", err
	}

	dstfile := filepath.Join(thecovpath, strconv.Itoa(pid)+".cov")
	return dstfile, nil
}

func start_inject(pid int) error {

	loggo.Info("start start_inject %d", pid)

	dstfile, err := get_pid_cov_file(pid)
	if err != nil {
		loggo.Error("get_pid_cov_file failed with %s", err)
		return err
	}

	lstatestr, err := get_lstate(pid)
	if err != nil {
		loggo.Error("get_lstate failed with %s", err)
		return err
	}

	// ./hookso call $PID libclua.so start_cov i=$L s="dst.cov" i=5
	cmd := exec.Command("bash", "-c", *hookso+" call "+strconv.Itoa(pid)+" "+*libclua+" start_cov i="+lstatestr+
		" s=\""+dstfile+"\" i="+strconv.Itoa(*covinter))
	_, err = cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s", err)
		return err
	}

	loggo.Info("end start_inject %d", pid)
	return nil
}

func save_source(gen_id bool) (map[string]SouceData, error) {
	loggo.Info("start save_source %s", *root)

	skippath := filepath.Join(*root, *skiproot)
	loggo.Info("save_source skip %s", skippath)

	bytes := 0
	sourcemap := make(map[string]SouceData)

	index := 0
	err := filepath.Walk(*root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			loggo.Error("prevent panic by handling failure accessing a path %q: %v", path, err)
			return err
		}

		if info.IsDir() && filepath.Clean(path) == filepath.Clean(skippath) {
			loggo.Info("skip path %s", filepath.Base(skippath))
			return filepath.SkipDir
		}

		if info == nil || info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(info.Name(), ".lua") {
			return nil
		}

		data, err := ioutil.ReadFile(path)
		if err != nil {
			loggo.Error("ioutil ReadFile fail %q: %v", path, err)
			return err
		}
		md5 := common.GetMd5String(string(data))

		sd := SouceData{string(data), md5, ""}
		if gen_id {
			sd.Id = strconv.Itoa(index) + "_" + strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
			index++
		}
		sourcemap[path] = sd
		bytes += len(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	loggo.Info("end save_source %s %d %d", *root, len(sourcemap), bytes)
	return sourcemap, nil
}

func reset_client() (map[string]SouceData, []int, error) {
	loggo.Info("start reset_client")
	pids, err := load_pids()
	if err != nil {
		loggo.Error("load_pids failed %s", err)
		return nil, nil, err
	}
	for _, pid := range pids {
		err := stop_inject(pid)
		if err != nil {
			loggo.Error("stop_inject failed %s", err)
			return nil, nil, err
		}
	}

	cursource, err := save_source(false)
	if err != nil {
		loggo.Error("save_source failed %s", err)
		return nil, nil, err
	}

	for _, pid := range pids {
		err := start_inject(pid)
		if err != nil {
			loggo.Error("start_inject failed %s", err)
			return nil, nil, err
		}
	}

	loggo.Info("end reset_client")
	return cursource, pids, nil
}

func get_cov_source_file(path string) ([]string, error) {

	// ./clua -path ./bin/ -i cov/4157.cov -showfunc=false -showtotal=false -showcode=false -showfile=true
	cmd := exec.Command("bash", "-c", *clua+" -path "+*root+" -i "+path+" -showfunc=false -showtotal=false -showcode=false -showfile=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s", err)
		return nil, err
	}

	var ret []string
	filestrs := strings.Split(string(out), "\n")
	for _, filestr := range filestrs {
		filestr = strings.TrimSpace(filestr)
		if len(filestr) <= 0 {
			continue
		}
		ret = append(ret, filestr)
	}
	return ret, nil
}

func backup_cov(pids []int) ([][]byte, map[string]int, error) {
	var ret [][]byte
	retsourcefile := make(map[string]int)
	for _, pid := range pids {
		src, err := get_pid_cov_file(pid)
		if err != nil {
			loggo.Error("get_pid_cov_file failed %s", err)
			return nil, nil, err
		}

		data, err := ioutil.ReadFile(src)
		if err != nil {
			loggo.Error("ioutil ReadFile fail %q: %v", src, err)
			return nil, nil, err
		}

		ret = append(ret, data)

		sourcefiles, err := get_cov_source_file(src)
		if err != nil {
			loggo.Error("get_cov_source_file fail %q: %v", src, err)
			return nil, nil, err
		}

		for _, sourcefile := range sourcefiles {
			retsourcefile[filepath.Clean(sourcefile)]++
		}
	}
	return ret, retsourcefile, nil
}

func send_to_server(covdata [][]byte, covsource map[string]int, source map[string]SouceData) error {

	tmpsource := make(map[string]SouceData)
	for k, v := range source {
		_, ok := covsource[filepath.Clean(k)]
		if ok {
			tmpsource[k] = v
		}
	}

	loggo.Info("start send_to_server %d %d %d", len(covdata), len(source), len(tmpsource))

	pushdata := PushData{covdata, tmpsource}

	b := bytes.Buffer{}
	e := gob.NewEncoder(&b)
	err := e.Encode(pushdata)
	if err != nil {
		loggo.Error("Encode fail %v", err)
		return err
	}
	data := string(b.Bytes())
	data = common.GzipStringBestCompression(data)
	data = base64.StdEncoding.EncodeToString([]byte(data))

	md5str := common.GetMd5String(data)

	loggo.Info("send_to_server data bytes %d %s", len(data), md5str)

	resp, err := http.PostForm(*server+"/coverage", url.Values{"md5": {md5str}, "data": {data}})
	if err != nil {
		loggo.Error("send_to_server fail %s", err)
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		loggo.Error("send_to_server fail %s", err)
		return err
	}

	loggo.Info("end send_to_server %s", string(body))

	return nil
}

func clear_invalid_file(pids []int) {

	filepath.Walk(*covpath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			loggo.Error("prevent panic by handling failure accessing a path %q: %v", path, err)
			return err
		}

		if info == nil || info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(info.Name(), ".cov") {
			return nil
		}
		find := false
		for _, pid := range pids {
			dst, err := get_pid_cov_file(pid)
			if err != nil {
				loggo.Error("get_pid_cov_file failed with %s", err)
				return err
			}
			if filepath.Clean(path) == filepath.Clean(dst) {
				find = true
			}
		}

		if !find {
			os.Remove(path)
		}
		return nil
	})

}

func ini_client() error {

	cursource, curpids, err := reset_client()
	if err != nil {
		loggo.Error("ini_client failed %s", err)
		return err
	}

	last := time.Now()
	for {
		if time.Now().Sub(last) < time.Minute {
			time.Sleep(time.Second)
			continue
		}
		last = time.Now()

		covdata, covsource, err := backup_cov(curpids)
		if err != nil {
			loggo.Error("backup_cov failed %s", err)
			return err
		}

		needreset := false

		newpids, err := load_pids()
		if err != nil {
			loggo.Error("load_pids failed %s", err)
			return err
		}

		for _, pid := range curpids {
			if !common.HasInt(newpids, pid) {
				loggo.Info("pid %d exit, need reset", pid)
				needreset = true
				break
			}
		}

		newsource, err := save_source(false)
		if err != nil {
			loggo.Error("save_source failed %s", err)
			return err
		}

		for path, newdata := range newsource {
			data, ok := cursource[path]
			if ok {
				if data.Md5sum != newdata.Md5sum {
					loggo.Info("file %s change, need reset", path)
					needreset = true
					break
				}
			}
		}

		if needreset {
			cursource, curpids, err = reset_client()
			if err != nil {
				loggo.Error("ini_client failed %s", err)
				return err
			}
			continue
		}

		for _, newpid := range newpids {
			if !common.HasInt(curpids, newpid) {
				err := start_inject(newpid)
				if err != nil {
					loggo.Error("start_inject failed %s", err)
					return err
				}
			}
		}

		send_to_server(covdata, covsource, cursource)

		curpids = newpids
		cursource = newsource

		clear_invalid_file(curpids)
	}

	return nil
}

/////////////////////////////////////////////////////////////////////////////////

var gpath map[string]func(*http.Request, http.ResponseWriter, string, url.Values)

func ini_server() error {

	http.HandleFunc("/", MyHandler)

	gpath = make(map[string]func(*http.Request, http.ResponseWriter, string, url.Values))
	gpath["/coverage"] = CoverageHandler

	loggo.Info("listen on " + strconv.Itoa(*port))
	err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)
	if err != nil {
		loggo.Error("ListenAndServe fail %v", err)
		return err
	}
	loggo.Info("quit")
	return nil
}

type Response struct {
	Code string `json:"code"`
	Data string `json:"data"`
}

func Res(w http.ResponseWriter, code string, data string) {

	res := Response{code, data}
	d, err := json.Marshal(res)
	if err != nil {
		loggo.Error("Res Marshal fail %v", err)
		return
	}
	if runtime.GOOS == "windows" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.Write(d)
}

func MyHandler(w http.ResponseWriter, r *http.Request) {
	loggo.Info("handle %v %v", r.Method, r.RequestURI)

	u, err := url.Parse(r.RequestURI)
	if err != nil {
		loggo.Error("Parse fail %v", r.RequestURI)
		Res(w, "wrong request", r.RequestURI)
		return
	}

	f, ok := gpath[u.Path]
	if !ok {
		loggo.Info("no path %v", u.Path)
		Res(w, "wrong request", u.Path)
		return
	}

	f(r, w, u.Path, u.Query())
}

func gen_data_filename() (string, error) {

	thecovpath, err := filepath.Abs(*covpath)
	if err != nil {
		loggo.Error("filepath Abs failed with %s", err)
		return "", err
	}

	err = os.MkdirAll(thecovpath, 0755)
	if err != nil {
		loggo.Error("os MkdirAll failed with %s", err)
		return "", err
	}

	filename := time.Now().Format("2006-01-02_15:04:05_") + common.UniqueId() + ".data"
	dstfile := filepath.Join(thecovpath, filename)
	return dstfile, nil
}

func CoverageHandler(r *http.Request, w http.ResponseWriter, path string, param url.Values) {
	md5str := r.FormValue("md5")
	data := r.FormValue("data")

	loggo.Info("CoverageHandler data %v %v", md5str, len(data))

	if md5str != common.GetMd5String(string(data)) {
		Res(w, "fail", "diff md5")
		return
	}

	filename, err := gen_data_filename()
	if err != nil {
		Res(w, "fail", err.Error())
		return
	}

	f, err := os.Create(filename)
	if err != nil {
		Res(w, "fail", err.Error())
		return
	}
	defer f.Close()

	_, err = f.WriteString(data)
	if err != nil {
		Res(w, "fail", err.Error())
		return
	}

	Res(w, "ok", "")

	loggo.Info("CoverageHandler %v", len(data))
}

/////////////////////////////////////////////////////////////////////////////////

func load_data_file_list() ([]string, error) {
	var ret []string
	filepath.Walk(*covpath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			loggo.Error("prevent panic by handling failure accessing a path %q: %v", path, err)
			return err
		}

		if info == nil || info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(info.Name(), ".data") {
			return nil
		}

		ret = append(ret, filepath.Clean(path))
		loggo.Info("load_data_file_list %s", filepath.Clean(path))

		return nil
	})

	return ret, nil
}

func write_tmp_file(data []byte) (string, error) {

	dstfile, err := gen_tmp_file("")
	if err != nil {
		loggo.Error("gen_tmp_file failed with %s", err)
		return "", err
	}

	f, err := os.Create(dstfile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = f.Write(data)
	if err != nil {
		return "", err
	}

	return dstfile, nil
}

func gen_tmp_file(filename string) (string, error) {

	thetmppath, err := filepath.Abs(*tmppath)
	if err != nil {
		loggo.Error("filepath Abs failed with %s", err)
		return "", err
	}

	err = os.MkdirAll(thetmppath, 0755)
	if err != nil {
		loggo.Error("os MkdirAll failed with %s", err)
		return "", err
	}

	if len(filename) <= 0 {
		filename = common.UniqueId()
	}
	filename += ".tmp"
	dstfile := filepath.Join(thetmppath, filename)
	return dstfile, nil
}

func lcov_add(covfile string, sourcefile string, id string) error {

	oldinfo, err := gen_tmp_file(id + ".info")
	if err != nil {
		loggo.Error("gen_tmp_file failed with %s", err)
		return err
	}

	if !common.FileExists(oldinfo) {
		// ./clua -path ./bin/ -i cov/4157.cov -fp sourcefile -lcov oldinfo.info -showfunc=false -showtotal=false -showcode=false -showfile=false
		cmd := exec.Command("bash", "-c", *clua+" -path "+*root+" -i "+covfile+" -fp "+sourcefile+
			" -lcov "+oldinfo+" -showfunc=false -showtotal=false -showcode=false -showfile=false")
		out, err := cmd.CombinedOutput()
		if err != nil {
			loggo.Error("exec Command failed with %s %s %s", string(out), err, oldinfo)
			return err
		}

		if !common.FileExists(oldinfo) {
			loggo.Error("lcov_add no oldinfo %s", oldinfo)
			return errors.New("no file")
		}

		loggo.Info("lcov_add new %s %s", sourcefile, oldinfo)
	} else {
		newinfo, err := gen_tmp_file(id + "_new.info")
		if err != nil {
			loggo.Error("gen_tmp_file failed with %s", err)
			return err
		}

		// ./clua -path ./bin/ -i cov/4157.cov -fp sourcefile -lcov newinfo.info -showfunc=false -showtotal=false -showcode=false -showfile=false
		cmd := exec.Command("bash", "-c", *clua+" -path "+*root+" -i "+covfile+" -fp "+sourcefile+
			" -lcov "+newinfo+" -showfunc=false -showtotal=false -showcode=false -showfile=false")
		_, err = cmd.CombinedOutput()
		if err != nil {
			loggo.Error("exec Command failed with %s", err)
			return err
		}
		loggo.Info("lcov add newinfo %s %s %s", covfile, sourcefile, newinfo)

		// lcov -a oldinfo.info -a newinfo.info -o oldinfo.info
		cmd = exec.Command("bash", "-c", *lcov+" -a "+oldinfo+" -a "+newinfo+" -o "+oldinfo)
		out, err := cmd.CombinedOutput()
		if err != nil {
			loggo.Error("exec Command failed with %s %s %s %s", string(out), err, oldinfo, newinfo)
			return err
		}

		if !common.FileExists(oldinfo) {
			loggo.Error("lcov_add no oldinfo %s", oldinfo)
			return errors.New("no file")
		}

		os.Remove(newinfo)

		loggo.Info("lcov_add ok %s %s", sourcefile, oldinfo)
	}

	return nil
}

func lcov_merge(covfile string, sourcefile string, source map[string]SouceData, id string) error {

	oldinfo, err := gen_tmp_file(id + ".info")
	if err != nil {
		loggo.Error("gen_tmp_file failed with %s", err)
		return err
	}

	sourcedata, ok := source[sourcefile]
	if !ok {
		loggo.Error("source no sourcefile %s", sourcefile)
		return err
	}

	oldsourcefile, err := write_tmp_file([]byte(sourcedata.Content))
	if err != nil {
		loggo.Error("write_tmp_file failed with %s", err)
		return err
	}

	difffile, err := gen_tmp_file("")
	if err != nil {
		loggo.Error("gen_tmp_file failed with %s", err)
		return err
	}

	// diff -u $PWD/old/prog.c $PWD/new/prog.c > diff
	cmd := exec.Command("bash", "-c", "diff -u "+oldsourcefile+" "+sourcefile+" > "+difffile)
	cmd.CombinedOutput()

	if !common.FileExists(difffile) {
		loggo.Error("lcov_add no difffile %s", oldinfo)
		return errors.New("no file")
	}
	loggo.Info("lcov_merge old sourcefile %s, new sourcefile %s, diff file %s", oldsourcefile, sourcefile, difffile)

	oldsourceinfo, err := gen_tmp_file(id + "_old.info")
	if err != nil {
		loggo.Error("gen_tmp_file failed with %s", err)
		return err
	}

	// ./clua -path ./bin/ -i cov/4157.cov -fp sourcefile -fpsource oldsourcefile -lcov oldsourceinfo.info -showfunc=false -showtotal=false -showcode=false -showfile=false
	cmd = exec.Command("bash", "-c", *clua+" -path "+*root+" -i "+covfile+" -fp "+sourcefile+" -fpsource "+oldsourcefile+
		" -lcov "+oldsourceinfo+" -showfunc=false -showtotal=false -showcode=false -showfile=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s %s %s", string(out), err, oldsourceinfo)
		return err
	}

	if !common.FileExists(oldsourceinfo) {
		loggo.Error("lcov_add no oldsourceinfo %s", oldsourceinfo)
		return errors.New("no file")
	}
	loggo.Info("lcov_merge old sourcefile %s, cov file %s, old info %s", oldsourcefile, covfile, oldsourceinfo)

	if !common.FileExists(oldinfo) {
		// lcov --diff oldsourceinfo.info difffile --convert-filenames -o oldinfo.info
		cmd = exec.Command("bash", "-c", *lcov+" --diff "+oldsourceinfo+" "+difffile+" --convert-filenames -o "+oldinfo)
		out, err := cmd.CombinedOutput()
		if err != nil {
			loggo.Error("exec Command failed with %s %s %s %s", string(out), err, oldsourceinfo, oldinfo)
			return err
		}

		err = common.FileReplace(oldinfo, "TN:,diff", "TN:")
		if err != nil {
			loggo.Error("FileReplace failed with %s", err)
			return err
		}
		err = common.FileReplace(oldinfo, "SF:"+oldsourcefile, "SF:"+sourcefile)
		if err != nil {
			loggo.Error("FileReplace failed with %s", err)
			return err
		}

		loggo.Info("lcov_merge new %s %s", sourcefile, oldinfo)
	} else {
		newinfo, err := gen_tmp_file(id + "_new.info")
		if err != nil {
			loggo.Error("gen_tmp_file failed with %s", err)
			return err
		}

		// lcov --diff oldsourceinfo.info difffile --convert-filenames -o newinfo.info
		cmd = exec.Command("bash", "-c", *lcov+" --diff "+oldsourceinfo+" "+difffile+" --convert-filenames -o "+newinfo)
		out, err := cmd.CombinedOutput()
		if err != nil {
			loggo.Error("exec Command failed with %s %s %s %s", string(out), err, oldsourceinfo, oldinfo)
			return err
		}

		err = common.FileReplace(newinfo, "TN:,diff", "TN:")
		if err != nil {
			loggo.Error("FileReplace failed with %s", err)
			return err
		}
		err = common.FileReplace(newinfo, "SF:"+oldsourcefile, "SF:"+sourcefile)
		if err != nil {
			loggo.Error("FileReplace failed with %s", err)
			return err
		}

		// lcov -a oldinfo.info -a newinfo.info -o oldinfo.info
		cmd = exec.Command("bash", "-c", *lcov+" -a "+oldinfo+" -a "+newinfo+" -o "+oldinfo)
		out, err = cmd.CombinedOutput()
		if err != nil {
			loggo.Error("exec Command failed with %s %s %s %s", string(out), err, oldinfo, newinfo)
			return err
		}

		if !common.FileExists(oldinfo) {
			loggo.Error("lcov_merge no oldinfo %s", oldinfo)
			return errors.New("no file")
		}

		os.Remove(newinfo)

		loggo.Info("lcov_merge ok %s %s", sourcefile, oldinfo)
	}

	os.Remove(oldsourcefile)
	os.Remove(difffile)
	os.Remove(oldsourceinfo)

	return nil
}

func gen_cov_sourcefile(covfile string, sourcefile string, source map[string]SouceData, cursource map[string]SouceData) error {
	rela, err := filepath.Rel(*root, sourcefile)
	if err != nil {
		loggo.Error("filepath Rel fail %s", err)
		return err
	}

	sourcedata, ok := source[sourcefile]
	if !ok {
		loggo.Info("cov %s no source file %s, skip", covfile, sourcefile)
		return nil
	}

	cursourcedata, ok := cursource[filepath.Clean(filepath.Join(*clientroot, rela))]
	if !ok {
		loggo.Info("current no source file %s, skip", sourcefile)
		return nil
	}

	if sourcedata.Md5sum == cursourcedata.Md5sum {
		return lcov_add(covfile, sourcefile, cursourcedata.Id)
	} else {
		return lcov_merge(covfile, sourcefile, source, cursourcedata.Id)
	}
}

func gen_covdata(covfile string, source map[string]SouceData, cursource map[string]SouceData) error {

	sourcelist, err := get_cov_source_file(covfile)
	if err != nil {
		loggo.Error("get_cov_source_file fail %v", err)
		return err
	}

	var errret error
	var count int32
	for _, file := range sourcelist {
		sourcefile := file
		if int(count) > *paralel {
			err := gen_cov_sourcefile(covfile, sourcefile, source, cursource)
			if err != nil {
				errret = err
			}
		} else {
			atomic.AddInt32(&count, 1)
			go func() {
				defer atomic.AddInt32(&count, -1)
				err := gen_cov_sourcefile(covfile, sourcefile, source, cursource)
				if err != nil {
					errret = err
				}
			}()
		}
		if errret != nil {
			loggo.Error("gen_cov_sourcefile fail %v", err)
			return err
		}
	}

	for count > 0 {
		time.Sleep(time.Second)
	}

	if errret != nil {
		return err
	}

	return nil
}

func merge_covdata_file(covdata [][]byte) (string, error) {
	var tmplist []string
	for _, data := range covdata {
		tmpfile, err := write_tmp_file(data)
		if err != nil {
			loggo.Error("write_tmp_file fail  %v", err)
			return "", err
		}
		tmplist = append(tmplist, tmpfile)
	}

	dst, err := gen_tmp_file("")
	if err != nil {
		loggo.Error("gen_tmp_file fail  %v", err)
		return "", err
	}

	params := ""
	for _, tmpfile := range tmplist {
		params += " -i " + tmpfile
	}
	params += " -o " + dst

	// ./clua -i a.cov -i b.cov -o dst.cov -path ./bin/ -showfunc=false -showtotal=false -showcode=false -showfile=false
	cmd := exec.Command("bash", "-c", *clua+params+" -path "+*root+" -showfunc=false -showtotal=false -showcode=false -showfile=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s %s %s", string(out), err, params)
		return "", err
	}

	if !common.FileExists(dst) {
		loggo.Error("merge_covdata_file no dst %s", dst)
		return "", errors.New("no file")
	}

	for _, tmpfile := range tmplist {
		os.Remove(tmpfile)
	}

	return dst, nil
}

func gen_data_file(filename string, cursource map[string]SouceData, index int, total int) error {

	filedata, err := ioutil.ReadFile(filename)
	if err != nil {
		loggo.Error("ioutil ReadFile fail %q: %v", filename, err)
		return err
	}
	data := string(filedata)

	filedata, err = base64.StdEncoding.DecodeString(data)
	if err != nil {
		loggo.Error("base64 DecodeString fail %q: %v", filename, err)
		return err
	}
	data = string(filedata)

	data = common.GunzipString(data)
	if len(data) <= 0 {
		loggo.Error("GunzipString fail %q: %v", filename, err)
		return err
	}

	b := bytes.Buffer{}
	_, err = b.WriteString(data)
	if err != nil {
		loggo.Error("Buffer WriteString fail %q: %v", filename, err)
		return err
	}

	e := gob.NewDecoder(&b)
	var pushdata PushData
	err = e.Decode(&pushdata)
	if err != nil {
		loggo.Error("Decode fail %v", err)
		return err
	}

	loggo.Info("read file %s %d %d %d/%d", filename, len(pushdata.Covdata), len(pushdata.Source), index+1, total)

	covfile, err := merge_covdata_file(pushdata.Covdata)
	if err != nil {
		loggo.Error("merge_covdata_file fail %v", err)
		return err
	}

	err = gen_covdata(covfile, pushdata.Source, cursource)
	if err != nil {
		loggo.Error("gen_covdata fail %q: %v", filename, err)
		return err
	}

	os.Remove(covfile)

	loggo.Info("gen file ok %s %d/%d", filename, index+1, total)

	return nil
}

func remove_all_tmp() {

	filepath.Walk(*tmppath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info == nil || info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(info.Name(), ".tmp") {
			return nil
		}

		os.Remove(path)
		return nil
	})
}

func merge_result_info(cursource map[string]SouceData) error {

	loggo.Info("start merge_result_info")

	params := ""

	resultfile, err := gen_tmp_file("")
	if err != nil {
		loggo.Error("gen_tmp_file failed with %s", err)
		return err
	}

	loggo.Info("merge_result_info resultfile %s", resultfile)

	for _, cursourcedata := range cursource {
		oldinfo, err := gen_tmp_file(cursourcedata.Id + ".info")
		if err != nil {
			loggo.Error("gen_tmp_file failed with %s", err)
			return err
		}

		if !common.FileExists(oldinfo) {
			continue
		}

		params += " -a " + oldinfo
	}

	params += " -o " + resultfile

	loggo.Info("merge_result_info params %s", params)

	// lcov -a a.info -a b.info -o resultfile.info
	cmd := exec.Command("bash", "-c", *lcov+params)
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s %s %s", string(out), err, params)
		return err
	}

	if !common.FileExists(resultfile) {
		loggo.Error("merge_result_info no resultfile %s %s", resultfile, params)
		return errors.New("no file")
	}

	loggo.Info("merge_result_info start genhtml %s", *htmloutputpath)

	// genhtml -o ./htmlout resultfile.info
	cmd = exec.Command("bash", "-c", *genhtml+" -o "+*htmloutputpath+" "+resultfile)
	out, err = cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s %s %s", string(out), err, resultfile)
		return err
	}

	loggo.Info("merge_result_info genhtml ok %s", *htmloutputpath)

	for _, cursourcedata := range cursource {
		oldinfo, err := gen_tmp_file(cursourcedata.Id + ".info")
		if err != nil {
			loggo.Error("gen_tmp_file failed with %s", err)
			return err
		}

		if !common.FileExists(oldinfo) {
			continue
		}

		os.Remove(oldinfo)
	}

	os.Remove(resultfile)

	return nil
}

func ini_gen() error {
	cursource, err := save_source(true)
	if err != nil {
		loggo.Error("save_source fail %v", err)
		return err
	}

	filelist, err := load_data_file_list()
	if err != nil {
		loggo.Error("load_data_file_list fail %v", err)
		return err
	}

	//remove_all_tmp()

	for index, filename := range filelist {
		err = gen_data_file(filename, cursource, index, len(filelist))
		if err != nil {
			loggo.Error("gen_data_file fail %v", err)
			return err
		}
	}

	err = merge_result_info(cursource)
	if err != nil {
		loggo.Error("merge_result_info fail %v", err)
		return err
	}

	return nil
}
