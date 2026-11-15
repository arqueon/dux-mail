import QtQuick
import qs.Common
import qs.Widgets

// Small pill for a thread label: leaf name over the label color (a Theme
// color when the label has none). `name` is the full path, meant for the
// tooltip; `text` overrides the visible text (used for the "+N" chip).
StyledRect {
    id: root

    property string name: ""
    property string text: ""
    property string colorBg: ""
    property string colorText: ""
    property bool clickable: false
    property int chipHeight: 18
    property real maxTextWidth: 120

    readonly property string shown: text !== "" ? text : name.slice(name.lastIndexOf("/") + 1)
    readonly property color bg: colorBg !== "" ? Qt.color(colorBg) : Theme.secondaryContainer
    readonly property color fg: colorBg === "" ? Theme.onSecondaryContainer : (colorText !== "" ? Qt.color(colorText) : Theme.bestTextOn(bg))

    signal clicked
    signal hoverChanged(bool hovered)

    implicitWidth: Math.min(maxTextWidth, label.implicitWidth) + Theme.spacingS * 2
    implicitHeight: chipHeight
    width: implicitWidth
    height: chipHeight
    radius: chipHeight / 2
    color: bg

    StyledText {
        id: label
        anchors.centerIn: parent
        width: Math.min(implicitWidth, root.maxTextWidth)
        text: root.shown
        elide: Text.ElideRight
        maximumLineCount: 1
        font.pixelSize: Theme.fontSizeSmall - 1
        color: root.fg
    }

    // Handlers (not a MouseArea) so a non-clickable chip never swallows the
    // click that selects the row underneath it.
    HoverHandler {
        cursorShape: root.clickable ? Qt.PointingHandCursor : Qt.ArrowCursor
        onHoveredChanged: root.hoverChanged(hovered)
    }

    TapHandler {
        enabled: root.clickable
        onTapped: root.clicked()
    }
}
