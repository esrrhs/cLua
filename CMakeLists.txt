cmake_minimum_required(VERSION 2.8)
project(clua)
aux_source_directory(. clua_src)

set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -Wall -g3 -std=c++11 -DNDEBUG")

add_library(clua SHARED ${clua_src})
