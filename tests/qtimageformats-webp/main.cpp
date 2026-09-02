#include <QByteArray>
#include <QColor>
#include <QCoreApplication>
#include <QImage>
#include <QString>
#include <QTextStream>
#include <QtGlobal>

namespace {

int fail(const QString &message)
{
    QTextStream(stderr) << "qtimageformats WebP test failed: " << message << Qt::endl;
    return 1;
}

bool pixelMatches(const QImage &image, int x, int y, int red, int green, int blue)
{
    const QColor color = image.pixelColor(x, y);
    return color.red() == red && color.green() == green && color.blue() == blue &&
        color.alpha() == 255;
}

} // namespace

int main(int argc, char *argv[])
{
    QCoreApplication application(argc, argv);
    application.setApplicationName(QStringLiteral("yaqt-qtimageformats-webp"));

    if (argc != 1) {
        return fail(QStringLiteral("expected no arguments"));
    }

    const QByteArray webPData = QByteArray::fromBase64(QByteArrayLiteral(
        "UklGRiwAAABXRUJQVlA4TB8AAAAvAUAAAB8gEEjeHzqN+RcQFPwf3fxHZA/gBgwR/Q8BAA=="));
    QImage image;
    if (!image.loadFromData(webPData, "WEBP")) {
        return fail(QStringLiteral("could not decode the embedded WebP image"));
    }
    if (image.width() != 2 || image.height() != 2) {
        return fail(QStringLiteral("decoded image has unexpected dimensions"));
    }
    if (!pixelMatches(image, 0, 0, 255, 0, 0) ||
        !pixelMatches(image, 1, 0, 0, 255, 0) ||
        !pixelMatches(image, 0, 1, 0, 0, 255) ||
        !pixelMatches(image, 1, 1, 255, 255, 255)) {
        return fail(QStringLiteral("decoded image has unexpected pixel colors"));
    }

    QTextStream(stdout) << "Decoded a 2x2 WebP image" << Qt::endl;
    return 0;
}
