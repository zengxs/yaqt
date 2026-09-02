cmake_minimum_required(VERSION 3.21)

project(yaqt_qtimageformats_webp LANGUAGES CXX)

find_package(Qt6 6.8 REQUIRED COMPONENTS Core Gui)

add_executable(yaqt-qtimageformats-webp main.cpp)

target_compile_features(yaqt-qtimageformats-webp PRIVATE cxx_std_17)
target_link_libraries(yaqt-qtimageformats-webp PRIVATE Qt6::Core Qt6::Gui)
