#include <string>
#include <list>
#include <vector>
#include <map>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>
#include <iostream>
#include <string>
#include <cstdlib>
#include <cstring>
#include <typeinfo>
#include <stdio.h>
#include <time.h>
#include <stdarg.h>
#include <assert.h>
#include <math.h>
#include <sys/time.h>
#include <signal.h>
#include <unistd.h>
#include <errno.h>
#include <unordered_map>
#include <fcntl.h>
#include <sstream>
#include <algorithm>
#include <vector>
#include <unordered_set>
#include <set>

extern "C" {
#include "lua.h"
#include "lualib.h"
#include "lauxlib.h"
}

const int open_debug = 0;

#define LLOG(...) if (open_debug) {llog("[DEBUG] ", __FILE__, __FUNCTION__, __LINE__, __VA_ARGS__);}
#define LERR(...) if (open_debug) {llog("[ERROR] ", __FILE__, __FUNCTION__, __LINE__, __VA_ARGS__);}

void llog(const char *header, const char *file, const char *func, int pos, const char *fmt, ...) {
    FILE *pLog = NULL;
    time_t clock1;
    struct tm *tptr;
    va_list ap;

    pLog = fopen("luacov.log", "a+");
    if (pLog == NULL) {
        return;
    }

    clock1 = time(0);
    tptr = localtime(&clock1);

    struct timeval tv;
    gettimeofday(&tv, NULL);

    fprintf(pLog, "===========================[%d.%d.%d, %d.%d.%d %llu]%s:%d,%s:===========================\n%s",
            tptr->tm_year + 1990, tptr->tm_mon + 1,
            tptr->tm_mday, tptr->tm_hour, tptr->tm_min,
            tptr->tm_sec, (long long) ((tv.tv_sec) * 1000 + (tv.tv_usec) / 1000), file, pos, func, header);

    va_start(ap, fmt);
    vfprintf(pLog, fmt, ap);
    fprintf(pLog, "\n\n");
    va_end(ap);

    va_start(ap, fmt);
    vprintf(fmt, ap);
    printf("\n\n");
    va_end(ap);

    fclose(pLog);
}

std::unordered_map <std::string, uint64_t> gdata;
int gcount;
int glasttime;
std::string gfile;
int gautosave;
int gpause;

static void flush_file(int fd, const char *buf, size_t len) {
    while (len > 0) {
        ssize_t r = write(fd, buf, len);
        buf += r;
        len -= r;
    }
}

static void flush() {
    int fd = open(gfile.c_str(), O_CREAT | O_WRONLY | O_TRUNC, 0666);
    if (fd < 0) {
        LERR("open file fail %s", gfile.c_str());
        return;
    }

    for (std::unordered_map<std::string, uint64_t>::iterator it = gdata.begin(); it != gdata.end(); it++) {
        int len = it->first.length();
        flush_file(fd, (const char *) &len, sizeof(len));
        flush_file(fd, it->first.c_str(), len);
        uint64_t count = it->second;
        flush_file(fd, (const char *) &count, sizeof(count));
    }

    close(fd);
}


static void hook_handler(lua_State *L, lua_Debug *par) {
    if (gpause) {
        return;
    }

    if (par->event != LUA_HOOKLINE) {
        LERR("hook_handler diff event %d", par->event);
        return;
    }

    lua_Debug ar;
    ar.source = 0;
    int ret = lua_getstack(L, 0, &ar);
    if (ret == 0) {
        LERR("hook_handler lua_getstack fail %d", ret);
        return;
    }

    ret = lua_getinfo(L, "S", &ar);
    if (ret == 0) {
        LERR("hook_handler lua_getinfo fail %d", ret);
        return;
    }
    if (ar.source == 0) {
        LERR("hook_handler source nil ");
        return;
    }
    if (ar.source[0] != '@') {
        LLOG("hook_handler source error %s ", ar.source);
        return;
    }

    char buff[128] = {0};
    snprintf(buff, sizeof(buff) - 1, "%d", par->currentline);
    std::string d = ar.source;
    d = d + ":";
    d = d + buff;

    gdata[d]++;

    if (gautosave > 0) {
        gcount++;
        if (gcount % 10000 == 0) {
            if (time(0) - glasttime > gautosave) {
                glasttime = time(0);
                LLOG("hook_handler %s %d", d.c_str(), gdata.size());
                flush();
            }
        }
    }
}

extern "C" void start_cov(lua_State *L, const char *file, int autosave) {
    if (gdata.size() > 0) {
        return;
    }
    gfile = file;
    gautosave = autosave;
    gcount = 0;
    glasttime = 0;
    lua_sethook(L, hook_handler, LUA_MASKLINE, 0);
}

extern "C" void stop_cov(lua_State *L) {
    lua_sethook(L, 0, 0, 0);
    flush();
    gdata.clear();
}

static int lpause(lua_State *L) {
    gpause = 1;
    return 0;
}

static int lresume(lua_State *L) {
    gpause = 0;
    return 0;
}

const char* default_file = "luacov.data";
static int lstart(lua_State *L) {
    const char *file = lua_tostring(L, 1);
    if (file == NULL) {
        file = default_file;
    }
    int autosave = (int) lua_tointeger(L, 2);
    start_cov(L, file, autosave);
    return 0;
}

static int lstop(lua_State *L) {
    stop_cov(L);
    return 0;
}

extern "C" int luaopen_libclua(lua_State *L) {
    luaL_checkversion(L);
    luaL_Reg l[] = {
            {"start", lstart},
            {"stop",  lstop},
            {"pause", lpause},
            {"resume", lresume},
            {NULL,    NULL},
    };
    luaL_newlib(L, l);
    return 1;
}
