#pragma once

#include <QObject>

class HelloObject final : public QObject
{
    Q_OBJECT

public:
    using QObject::QObject;
};
