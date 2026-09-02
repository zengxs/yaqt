#include "hello_object.h"

#include <QApplication>
#include <QByteArray>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QLabel>
#include <QLibraryInfo>
#include <QString>
#include <QTextStream>
#include <QtGlobal>

namespace {

QString canonicalPath(const QString &path)
{
    const QFileInfo info(path);
    const QString canonical = info.canonicalFilePath();
    return QDir::cleanPath(canonical.isEmpty() ? info.absoluteFilePath() : canonical);
}

bool pathsEqual(const QString &left, const QString &right)
{
#ifdef Q_OS_WIN
    return left.compare(right, Qt::CaseInsensitive) == 0;
#else
    return left == right;
#endif
}

int fail(const QString &message)
{
    QTextStream(stderr) << "CMake Hello failed: " << message << Qt::endl;
    return 1;
}

} // namespace

int main(int argc, char *argv[])
{
    QApplication application(argc, argv);
    application.setApplicationName(QStringLiteral("yaqt-cmake-hello"));

    if (argc != 2) {
        return fail(QStringLiteral("expected PREFIX"));
    }

    const QString runtimeVersion = QString::fromLatin1(qVersion());
    const QString compileVersion = QString::fromLatin1(QT_VERSION_STR);
    if (runtimeVersion != compileVersion) {
        return fail(
            QStringLiteral("compiled with Qt %1 but loaded Qt %2")
                .arg(compileVersion, runtimeVersion));
    }

    const QString expectedPrefix = canonicalPath(QString::fromLocal8Bit(argv[1]));
    const QString runtimePrefix = canonicalPath(
        QLibraryInfo::path(QLibraryInfo::PrefixPath));
    if (!pathsEqual(runtimePrefix, expectedPrefix)) {
        return fail(
            QStringLiteral("runtime prefix is %1, expected %2")
                .arg(runtimePrefix, expectedPrefix));
    }

    QFile resource(QStringLiteral(":/cmake-hello/message.txt"));
    if (!resource.open(QIODevice::ReadOnly)) {
        return fail(QStringLiteral("could not open the embedded resource"));
    }
    const QByteArray message = resource.readAll().trimmed();
    if (message != QByteArrayLiteral("Hello from Qt")) {
        return fail(QStringLiteral("embedded resource contents do not match"));
    }

    HelloObject helloObject;
    if (QString::fromLatin1(helloObject.metaObject()->className()) !=
        QStringLiteral("HelloObject")) {
        return fail(QStringLiteral("Qt meta-object code was not generated"));
    }

    QLabel window(QString::fromUtf8(message));
    window.setWindowTitle(QStringLiteral("yaqt CMake Hello"));
    window.resize(320, 120);
    window.show();
    QApplication::processEvents();

    QTextStream(stdout)
        << "Qt " << runtimeVersion << " at " << runtimePrefix << Qt::endl;
    return 0;
}
