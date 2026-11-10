cmake_minimum_required(VERSION 3.13)
project(clua)

set(CMAKE_CXX_STANDARD 11)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -Wall -g3 -DNDEBUG")

# Locate lua.h so a clean checkout works on distros that install
# headers under lua5.3/ or lua5.4/. Override with -DLUA_INCLUDE_DIR=...
find_path(LUA_INCLUDE_DIR lua.h
  HINTS
    "${LUA_INCLUDE_DIR}"
    "$ENV{LUA_INCLUDE_DIR}"
    "$ENV{LUA_DIR}/include"
  PATHS
    /usr/local/include
    /usr/include
  PATH_SUFFIXES
    lua
    lua5.4
    lua5.3
)
if(NOT LUA_INCLUDE_DIR)
  message(FATAL_ERROR "lua.h not found. Install Lua 5.3+ development headers or pass -DLUA_INCLUDE_DIR=<dir>")
endif()
message(STATUS "Found Lua headers: ${LUA_INCLUDE_DIR}")

add_library(clua SHARED main.cpp)
set_target_properties(clua PROPERTIES
  POSITION_INDEPENDENT_CODE ON
  PREFIX "lib"
  OUTPUT_NAME "clua"
)
target_include_directories(clua PRIVATE ${LUA_INCLUDE_DIR})
