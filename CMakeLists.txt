cmake_minimum_required(VERSION 2.8)
project(luacov)
aux_source_directory(. luacov_src)

set(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -Wall -g3 -std=c++11 -DNDEBUG")

add_library(luacov SHARED ${luacov_src})
