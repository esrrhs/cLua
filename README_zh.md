# cLua

[English](README.md) | [简体中文](README_zh.md)

[<img src="https://img.shields.io/github/license/esrrhs/cLua">](https://github.com/esrrhs/cLua)
[<img src="https://img.shields.io/github/languages/top/esrrhs/cLua">](https://github.com/esrrhs/cLua)
[<img src="https://img.shields.io/github/actions/workflow/status/esrrhs/cLua/go.yml?branch=master">](https://github.com/esrrhs/cLua/actions)

cLua 是一个针对 Lua 的高性能代码覆盖率统计工具（支持 Lua 5.3+）。通过 C++ 底层采集引擎与 Go 语言语法解析工具，提供精准、低开销的代码覆盖率采集与分析能力。

---

## 目录

- [特性](#特性)
- [架构流程](#架构流程)
- [环境要求](#环境要求)
- [编译构建](#编译构建)
- [使用方法](#使用方法)
  - [方式一：在 Lua 脚本中直接嵌入](#方式一在-lua-脚本中直接嵌入)
  - [方式二：通过 hookso 动态注入到运行中进程](#方式二通过-hookso-动态注入到运行中进程)
- [结果解析与可视化](#结果解析与可视化)
  - [终端控制台查看](#终端控制台查看)
  - [生成 LCOV 与 HTML 图形化报告](#生成-lcov-与-html-图形化报告)
- [自动化覆盖率服务 (CluaHelper)](#自动化覆盖率服务-cluahelper)
- [相关项目](#相关项目)
- [开源协议](#开源协议)

---

## 特性

* 🚀 **高性能、低开销**：数据采集核心采用 C++ 编写，挂载轻量级 Lua Hook，对宿主进程的运行性能影响降到最低。
* 🔌 **使用灵活**：支持在脚本中直接 `require "libclua"` 引入，也可通过 [hookso](https://github.com/esrrhs/hookso) 动态注入到正在运行的生产/测试进程中，无需修改源代码或重启服务。
* 🎯 **精确语法分析**：Go 语言编写的解析器通过解析 Lua AST 语法树，精准计算文件级别与各个函数级别的覆盖率百分比与执行行数。
* ⏸️ **暂停与恢复**：支持 `cl.pause()` 与 `cl.resume()`，可灵活过滤不需要统计的代码段。
* 📊 **丰富可视化展示**：支持输出标准的 [LCOV](http://ltp.sourceforge.net/coverage/lcov.php) 格式数据，配合 `genhtml` 即可一键生成直观易懂的 HTML 覆盖率分析报告。
* 🌐 **自动化服务搭建**：配套提供 `clua_helper`，涵盖客户端（自动注入与定时上报）、服务端（数据接收与网页托管）、生成器（合并覆盖率并生成静态页），方便在 CI/CD 中落地。

---

## 架构流程

```text
+-----------------------+
|  Target Process (Lua) |
|  +-----------------+  |
|  |   libclua.so    |  |  == (write coverage) ==>  test.cov (Binary Data)
|  +-----------------+  |                                  |
+-----------------------+                                  |
                                                           v
                                                  +-----------------+
                                                  |   clua (CLI)    |
                                                  +-----------------+
                                                     |           |
                                    (Terminal Stats) v           v (Export LCOV)
                                       Function & File       test.info
                                       Coverage Rates            |
                                                                 v (genhtml)
                                                           HTML Dashboard
```

---

## 环境要求

* **OS**: Linux (x86_64)
* **Lua**: 5.3 或更高版本（需安装 Lua 开发头文件及动态库，如 `lua.h`）
* **C++ 编译器**: GCC / Clang（支持 C++11 及以上）
* **CMake**: 2.8 及以上版本
* **Go**: 1.19 及以上版本
* **LCOV**（可选，用于生成 HTML 图表）：可通过 `apt install lcov` 或 `yum install lcov` 安装

---

## 编译构建

### 1. 编译 C++ 采集动态库 (`libclua.so`)
```bash
cmake .
make
```
编译完成后，会在当前目录生成 `libclua.so`。

### 2. 编译覆盖率解析工具 (`clua`)
```bash
go build clua.go
```
编译完成后，生成用于解析覆盖率文件的命令行工具 `clua`。

### 3. 编译覆盖率服务组件 (`clua_helper`)
```bash
go build clua_helper.go
```
编译完成后，生成分布式覆盖率服务工具 `clua_helper`。

---

## 使用方法

### 方式一：在 Lua 脚本中直接嵌入

在需要统计覆盖率的 Lua 脚本中载入并启动采集：

```lua
-- 1. 加载 libclua.so
local cl = require "libclua"

-- 2. 开始记录覆盖率
-- 参数 1: 结果数据文件的保存路径（默认为 "luacov.data"）
-- 参数 2: 定时自动刷新落盘的间隔时间（秒），0 表示不自动定时落盘，仅在 stop 时写入
cl.start("test.cov", 5)

-- （可选）支持在关键区间进行暂停与恢复
-- cl.pause()
-- cl.resume()

-- 3. 执行业务代码逻辑
do_something()

-- 4. 结束统计并刷新数据到文件
cl.stop()
```

> **提示**：运行 Lua 时请确保 `LUA_CPATH` 能找到 `libclua.so`，例如：`LUA_CPATH="./?.so;;" lua test.lua`。

---

### 方式二：通过 hookso 动态注入到运行中进程

利用 [hookso](https://github.com/esrrhs/hookso) 工具，可以在不重启进程、不改动业务代码的前提下，随时对运行中的 Lua 宿主进程开启/关闭覆盖率采集：

1. **获取宿主进程中的 `lua_State*` 指针**：
   例如通过进程调用的 `lua_settop(L)` 函数获取第一个参数地址（假设进程 PID 为 `$PID`）：
   ```bash
   ./hookso arg $PID liblua.so lua_settop 1
   # 输出示例: 123456
   ```

2. **注入并加载 `libclua.so`**：
   ```bash
   ./hookso dlopen $PID ./libclua.so
   ```

3. **调用 `start_cov` 开启统计**：
   等价于在进程内执行 `start_cov(L, "./test.cov", 5)`：
   ```bash
   ./hookso call $PID libclua.so start_cov i=123456 s="./test.cov" i=5
   ```

4. **调用 `stop_cov` 关闭统计并保存数据**：
   等价于在进程内执行 `stop_cov(L)`：
   ```bash
   ./hookso call $PID libclua.so stop_cov i=123456
   ```

---

## 结果解析与可视化

### 终端控制台查看

使用编译出的 `clua` 命令行工具解析生成的覆盖率文件：

```bash
./clua -i test.cov
```

输出示例：
```text
total points = 27, files = 1
coverage of /home/project/cLua/test.lua:
    local cl = require "libclua"
    cl.start("test.cov", 5)
    
1   function test1(i)
10      if i % 2 then
10          print("a "..i)
        else
            print("b "..i)
        end
11  end
    
1   function test2(i)
40      if i > 30 then
19          print("c "..i)
        else
21          print("d "..i)
        end
41  end
    
1   function test3(i)
    
51      if i > 0 then
51          print("e "..i)
        else
            print("f "..i)
        end
    
52  end
    
    test4 = function(i)
    
        local function test5(i)
12          print("g "..i)
15      end
    
15      for i = 0, 3 do
12          test5(i)
        end
    
4   end
    
102 for i = 0, 100 do
101     if i < 10 then
10          test1(i)
91      elseif i < 50 then
40          test2(i)
        else
51          test3(i)
        end
    end
    
4   for i = 0, 2 do
3       test4(i)
    end
    
1   cl.stop()
    
/home/project/cLua/test.lua total coverage 78% 22/28
/home/project/cLua/test.lua function coverage [function test1(i)] 66% 2/3
/home/project/cLua/test.lua function coverage [function test2(i)] 100% 3/3
/home/project/cLua/test.lua function coverage [function test3(i)] 66% 2/3
/home/project/cLua/test.lua function coverage [test4 = function(i)] 75% 3/4
/home/project/cLua/test.lua function coverage [local function test5(i)] 100% 1/1
```
每行代码前的数字表示该行的执行命中次数；空白表示未被执行。末尾会详细列出文件整体覆盖率以及每个函数的覆盖率统计。

---

### 生成 LCOV 与 HTML 图形化报告

1. 将覆盖率文件转换为 LCOV 标准格式：
   ```bash
   ./clua -i test.cov -lcov test.info
   ```

2. 使用 lcov 自带的 `genhtml` 工具生成网页：
   ```bash
   genhtml -o htmltest test.info
   ```

3. 打开 `htmltest/index.html` 查看整体报告：
   ![LCOV Overview](./lcov1.png)

4. 点击对应源文件进入行级详情视图：
   ![LCOV Detail](./lcov2.png)

> **小贴士**：LCOV 还支持将多个 `.info` 文件进行合并（`lcov -a file1.info -a file2.info -o merged.info`），便于聚合多次测试运行的结果。

---

## 自动化覆盖率服务 (CluaHelper)

`clua_helper` 提供了覆盖率自动化收集与服务体系，包含**客户端 (Client)**、**服务端 (Server)**、**生成器 (Gen)** 三种运行模式。可通过 `./clua_helper -h` 查看所有支持的参数。

### 1. CluaHelper 客户端
在运行目标服务的机器上启动，负责搜索宿主进程、自动注入 `libclua.so`、监控代码路径变动并定时将采集结果上报给服务端：
```bash
./clua_helper -type client \
  -bin 宿主二进制名字 \
  -getluastate "获取LuaState的指令，如：liblua.so lua_settop 1" \
  -path 代码目录 \
  -server http://server_ip:8877
```

### 2. CluaHelper 服务端
部署在数据汇总服务器上，负责接收所有客户端上报的覆盖率文件，保存到本地，并对外提供静态网页访问服务：
```bash
./clua_helper -type server -port 8877
```

### 3. CluaHelper 生成器
在汇总服务器上运行，负责读取服务端收集到的各个结果文件，与本地代码进行合并，最后生成供网页展示的 HTML 目录：
```bash
./clua_helper -type gen \
  -covpath 服务端保存的结果目录 \
  -path 本地代码目录 \
  -clientpath 客户端代码目录
```

生成完毕后，访问 `http://server_ip:8877/static/` 即可通过浏览器直接查看覆盖率报告：
![LCOV Web Report](./lcov1.png)

---

## 相关项目

* [hookso](https://github.com/esrrhs/hookso): Linux 动态库注入与函数调用工具
* [lua全家桶](https://github.com/esrrhs/lua-family-bucket): 汇集各类 Lua 工具与库

---

## 开源协议

本项目基于 [MIT 许可证](LICENSE) 开源。
