package main

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"flag"
	"github.com/esrrhs/go-engine/src/common"
	"github.com/esrrhs/go-engine/src/fastwalk"
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
	"sync"
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
var lcov = flag.String("lcov", "./lcov", "lcov bin path")
var paralel = flag.Int("paralel", 8, "max paralel")
var clientroot = flag.String("clientpath", "./", "client source code path")
var genhtml = flag.String("genhtml", "./genhtml", "genhtml bin path")
var htmloutputpath = flag.String("htmlout", "./htmlout", "html output path")
var deletecov = flag.Bool("deletecovpath", true, "delete coverage path data")
var resultdata = flag.String("resultdata", "", "save result data file path")
var lastresultdata = flag.String("lastresultdata", "", "merge last save result data file path")
var checkinter = flag.Int("checkinter", 60, "client check inter in second")
var sendinter = flag.Int("sendinter", 3600, "client send inter in second")
var statichtml = flag.String("statichtml", "static", "static html prefix")

func main() {

	defer common.CrashLog()

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
		loggo.Error("exec Command failed with %s %s", err, string(out))
		return "", err
	}
	lstatestr := string(out)
	lstatestr = strings.TrimSpace(lstatestr)
	loggo.Info("pid %d L = %s", pid, lstatestr)

	// ./hookso dlopen $PID ./libclua.so
	cmd = exec.Command("bash", "-c", *hookso+" dlopen "+strconv.Itoa(pid)+" "+*libclua)
	out, err = cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s %s", err, string(out))
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s %s", err, string(out))
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s %s", err, string(out))
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

	var mu sync.Mutex

	index := 0
	fun := func(path string, typ os.FileMode) error {

		if typ&os.ModeSymlink == os.ModeSymlink {
			return fastwalk.TraverseLink
		}

		if typ.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".lua") {
			return nil
		}

		if strings.HasPrefix(filepath.Clean(path), filepath.Clean(skippath)) {
			//loggo.Info("skip path %s %s %s", path, filepath.Clean(path), filepath.Base(skippath))
			return nil
		}

		data, err := ioutil.ReadFile(path)
		if err != nil {
			loggo.Error("ioutil ReadFile fail %q: %v", path, err)
			return err
		}
		md5 := common.GetMd5String(string(data))

		mu.Lock()
		defer mu.Unlock()

		sd := SouceData{string(data), md5, ""}
		if gen_id {
			name := filepath.Base(path)
			sd.Id = strconv.Itoa(index) + "_" + strings.TrimSuffix(name, filepath.Ext(name))
			index++
		}

		//loggo.Info("add sourcemap %s", filepath.Clean(path))

		sourcemap[filepath.Clean(path)] = sd
		bytes += len(data)
		return nil
	}

	err := fastwalk.Walk(*root, fun)
	if err != nil {
		loggo.Error("godirwalk Walk %s", err)
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

func make_push_data(covdata [][]byte, covsource map[string]int, source map[string]SouceData) (string, error) {

	tmpsource := make(map[string]SouceData)
	for k, v := range source {
		_, ok := covsource[filepath.Clean(k)]
		if ok {
			tmpsource[k] = v
		}
	}

	loggo.Info("make_push_data %d %d", len(covdata), len(tmpsource))

	pushdata := PushData{covdata, tmpsource}

	b := bytes.Buffer{}
	e := gob.NewEncoder(&b)
	err := e.Encode(pushdata)
	if err != nil {
		loggo.Error("Encode fail %v", err)
		return "", err
	}
	data := string(b.Bytes())
	data = common.GzipStringBestCompression(data)
	data = base64.StdEncoding.EncodeToString([]byte(data))

	return data, nil
}

func send_to_server(covdata [][]byte, covsource map[string]int, source map[string]SouceData) error {

	loggo.Info("start send_to_server %d %d", len(covdata), len(source))

	data, err := make_push_data(covdata, covsource, source)
	if err != nil {
		loggo.Error("make_push_data fail %v", err)
		return err
	}

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
	for {
		err := run_client()
		if err != nil {
			time.Sleep(time.Second * 10)
		}
	}
}

func run_client() error {

	cursource, curpids, err := reset_client()
	if err != nil {
		loggo.Error("ini_client failed %s", err)
		return err
	}

	var tosend_covdata [][]byte
	var tosend_covsource map[string]int
	var tosend_cursource map[string]SouceData

	last := time.Now()
	lastsend := time.Now()
	for {
		if time.Now().Sub(last) < time.Second*time.Duration(*checkinter) {
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

			loggo.Info("start send per hour")
			if tosend_covdata != nil && tosend_covsource != nil && tosend_cursource != nil {
				send_to_server(tosend_covdata, tosend_covsource, tosend_cursource)
				lastsend = time.Now()
				tosend_covdata = nil
				tosend_covsource = nil
				tosend_cursource = nil
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

		loggo.Info("everything ok")

		tosend_covdata = covdata
		tosend_covsource = covsource
		tosend_cursource = cursource

		if time.Now().Sub(lastsend) >= time.Second*time.Duration(*sendinter) {
			loggo.Info("start send per hour")
			send_to_server(tosend_covdata, tosend_covsource, tosend_cursource)
			lastsend = time.Now()
		}

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

	fs := http.FileServer(http.Dir(*htmloutputpath))
	http.Handle("/"+*statichtml+"/", http.StripPrefix("/"+*statichtml+"/", fs))

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

func load_data_file_list() ([]string, string, error) {
	var ret []string

	retlastresultdataabs := ""
	if len(*lastresultdata) != 0 {
		lastresultdataabs, err := filepath.Abs(*lastresultdata)
		if err != nil {
			loggo.Error("gen_tmp_file failed with %s", err)
			return nil, "", err
		}

		if !common.FileExists(lastresultdataabs) {
			loggo.Error("last resultdata not find %s", lastresultdataabs)
			return nil, "", err
		}

		ret = append(ret, lastresultdataabs)
		retlastresultdataabs = lastresultdataabs
	}

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

	return ret, retlastresultdataabs, nil
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

func lcov_merge(covfile string, sourcefile string, clientsoucefile string, source map[string]SouceData, id string) error {

	oldinfo, err := gen_tmp_file(id + ".info")
	if err != nil {
		loggo.Error("gen_tmp_file failed with %s", err)
		return err
	}

	sourcedata, ok := source[clientsoucefile]
	if !ok {
		loggo.Error("source no soucefile %s", clientsoucefile)
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

	clientsoucefile := filepath.Clean(filepath.Join(*clientroot, rela))

	sourcedata, ok := source[clientsoucefile]
	if !ok {
		loggo.Info("cov %s no source file %s, skip", covfile, sourcefile)
		return nil
	}

	cursourcedata, ok := cursource[sourcefile]
	if !ok {
		loggo.Info("current no source file %s %s %s %s, skip", sourcefile, rela, filepath.Join(*clientroot, rela), filepath.Clean(filepath.Join(*clientroot, rela)))
		return nil
	}

	if sourcedata.Md5sum == cursourcedata.Md5sum {
		return lcov_add(covfile, sourcefile, cursourcedata.Id)
	} else {
		return lcov_merge(covfile, sourcefile, clientsoucefile, source, cursourcedata.Id)
	}
}

func gen_covdata(covfile string, source map[string]SouceData, cursource map[string]SouceData) error {

	loggo.Info("start gen_covdata %s", covfile)

	sourcelist, err := get_cov_source_file(covfile)
	if err != nil {
		loggo.Error("get_cov_source_file fail %v", err)
		return err
	}

	loggo.Info("gen_covdata get_cov_source_file %s %d", covfile, len(sourcelist))

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

	if len(covdata) <= 0 {
		loggo.Info("merge_covdata_file no data")
		return "", errors.New("no file")
	}

	if len(covdata) == 1 {
		tmpfile, err := write_tmp_file(covdata[0])
		if err != nil {
			loggo.Error("write_tmp_file fail  %v", err)
			return "", err
		}
		loggo.Info("merge_covdata_file no need, only one %s", tmpfile)
		return tmpfile, nil
	}

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

	loggo.Info("merge_covdata_file ok %s", dst)

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

func save_resultfile(resultfile string, cursource map[string]SouceData) error {

	loggo.Info("start save_resultfile %s", resultfile)

	covflle, err := gen_tmp_file("")
	if err != nil {
		loggo.Error("gen_tmp_file failed with %s", err)
		return err
	}

	// ./clua -path root -i test1.cov -lcov test.info -reverse
	cmd := exec.Command("bash", "-c", *clua+" -path "+*root+" -i "+covflle+" -lcov "+resultfile+" -reverse"+"")
	out, err := cmd.CombinedOutput()
	if err != nil {
		loggo.Error("exec Command failed with %s %s %s", string(out), err, covflle)
		return err
	}

	if !common.FileExists(covflle) {
		loggo.Error("save_resultfile no covflle %s", covflle)
		return errors.New("no file")
	}

	loggo.Info("save_resultfile gen cov file %s", covflle)

	covdata, err := ioutil.ReadFile(covflle)
	if err != nil {
		loggo.Error("ioutil ReadFile fail %q: %v", covflle, err)
		return err
	}

	retsourcefile := make(map[string]int)
	sourcefiles, err := get_cov_source_file(covflle)
	if err != nil {
		loggo.Error("get_cov_source_file fail %q: %v", covflle, err)
		return err
	}

	for _, sourcefile := range sourcefiles {
		retsourcefile[filepath.Clean(sourcefile)]++
	}

	filename := *resultdata
	if len(filename) <= 0 {
		filename = time.Now().Format("2006-01-02_15:04:05_") + common.UniqueId() + ".data"
	}

	loggo.Info("save_resultfile start compress data %s", filename)

	covdatas := make([][]byte, 0)
	covdatas = append(covdatas, covdata)

	tmpsource := make(map[string]SouceData)
	for k, v := range cursource {
		v.Id = ""
		tmpsource[k] = v
	}

	data, err := make_push_data(covdatas, retsourcefile, tmpsource)
	if err != nil {
		loggo.Error("make_push_data fail %v", err)
		return err
	}

	loggo.Info("save_resultfile start write data %s %d", filename, len(data))

	f, err := os.Create(filename)
	if err != nil {
		loggo.Error("Create fail %v", err)
		return err
	}
	defer f.Close()

	_, err = f.WriteString(data)
	if err != nil {
		loggo.Error("WriteString fail %v", err)
		return err
	}

	os.Remove(covflle)

	loggo.Info("save_resultfile ok %s %d", filename, len(data))

	return nil
}

func merge_result_info(cursource map[string]SouceData) error {

	loggo.Info("start merge_result_info")

	var inputlist []string

	n := 0
	for _, cursourcedata := range cursource {
		oldinfo, err := gen_tmp_file(cursourcedata.Id + ".info")
		if err != nil {
			loggo.Error("gen_tmp_file failed with %s", err)
			return err
		}

		if !common.FileExists(oldinfo) {
			continue
		}

		inputlist = append(inputlist, oldinfo)
		n++
	}

	if n > 0 {

		start := 0

		var lastresultfile string

		for {
			params := ""

			resultfile, err := gen_tmp_file("")
			if err != nil {
				loggo.Error("gen_tmp_file failed with %s", err)
				return err
			}

			loggo.Info("merge_result_info resultfile %s", resultfile)

			for i := 0; i < 10 && start < len(inputlist); i++ {
				if common.FileFind(inputlist[start], "DA:") > 0 {
					params += " -a " + inputlist[start]
				}
				start++
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

			if start < len(inputlist) {
				inputlist = append(inputlist, resultfile)
			} else {
				lastresultfile = resultfile
				break
			}
		}

		loggo.Info("merge_result_info start genhtml %s", *htmloutputpath)

		// genhtml -o ./htmlout resultfile.info
		cmd := exec.Command("bash", "-c", *genhtml+" -o "+*htmloutputpath+" "+lastresultfile)
		out, err := cmd.CombinedOutput()
		if err != nil {
			loggo.Error("exec Command failed with %s %s %s", string(out), err, lastresultfile)
			return err
		}

		loggo.Info("merge_result_info genhtml ok %s", *htmloutputpath)

		err = save_resultfile(lastresultfile, cursource)
		if err != nil {
			loggo.Error("save_resultfile failed with %s", err)
			return err
		}

		os.Remove(lastresultfile)

	} else {
		loggo.Info("no info, merge_result_info skip %s", *htmloutputpath)
	}

	for _, oldinfo := range inputlist {
		os.Remove(oldinfo)
	}

	return nil
}

func ini_gen() error {
	cursource, err := save_source(true)
	if err != nil {
		loggo.Error("save_source fail %v", err)
		return err
	}

	filelist, lastresult_filename, err := load_data_file_list()
	if err != nil {
		loggo.Error("load_data_file_list fail %v", err)
		return err
	}

	remove_all_tmp()

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

	if *deletecov {
		for _, filename := range filelist {
			if filename != lastresult_filename {
				os.Remove(filename)
			}
		}
	}

	return nil
}
