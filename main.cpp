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
const int MAX_NAME_SIZE = 128;
std::string gfile;
int gautosave;

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

    struct Save {
        char name[MAX_NAME_SIZE];
        uint64_t count;
    };
    const int MAX_SAVE_NUM = 1024;
    Save save[MAX_SAVE_NUM];
    int savenum = 0;
    for (std::unordered_map<std::string, uint64_t>::iterator it = gdata.begin(); it != gdata.end(); it++) {
        if (savenum >= MAX_SAVE_NUM) {
            flush_file(fd, (const char *) &save, sizeof(save));
            memset(&save, sizeof(save), 0);
            savenum = 0;
        }
        strncpy(save[savenum].name, it->first.c_str(), MAX_NAME_SIZE - 1);
        save[savenum].count = it->second;
        savenum++;
    }
    if (savenum > 0) {
        flush_file(fd, (const char *) &save, sizeof(save));
        memset(&save, sizeof(save), 0);
        savenum = 0;
    }
}

static void hook_handler(lua_State *L, lua_Debug *par) {
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

    char buff[MAX_NAME_SIZE] = {0};
    snprintf(buff, sizeof(buff) - 1, "%s:%d", ar.source, par->currentline);

    gdata[buff]++;

    if (gautosave > 0) {
        gcount++;
        if (gcount % 10000 == 0) {
            if (time(0) - glasttime > gautosave) {
                glasttime = time(0);
                LLOG("hook_handler %s %d", buff, gdata.size());
                flush();
            }
        }
    }
}

extern "C" void start_cov(lua_State *L) {
    if (gdata.size() > 0) {
        return;
    }
    gcount = 0;
    glasttime = 0;
    lua_sethook(L, hook_handler, LUA_MASKLINE, 0);
}

extern "C" void stop_cov(lua_State *L) {
    lua_sethook(L, 0, 0, 0);
    flush();
    gdata.clear();
}

static int lstart(lua_State *L) {
    const char *file = lua_tostring(L, 1);
    int autosave = (int) lua_tointeger(L, 2);
    gfile = file;
    gautosave = autosave;
    start_cov(L);
    return 0;
}

static int lstop(lua_State *L) {
    stop_cov(L);
    return 0;
}

extern "C" int luaopen_libluacov(lua_State *L) {
    luaL_checkversion(L);
    luaL_Reg l[] = {
            {"start", lstart},
            {"stop",  lstop},
            {NULL,    NULL},
    };
    luaL_newlib(L, l);
    return 1;
}
