cmake_minimum_required(VERSION 3.21)

project(yaqt_cmake_hello LANGUAGES CXX)

set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)

find_package(Qt6 6.8 REQUIRED COMPONENTS Core Gui Widgets)

add_executable(yaqt-cmake-hello
    hello_object.h
    main.cpp
    resources.qrc
)

target_compile_features(yaqt-cmake-hello PRIVATE cxx_std_17)
target_link_libraries(yaqt-cmake-hello PRIVATE Qt6::Core Qt6::Gui Qt6::Widgets)
