import QtQuick
import Quickshell
import qs.Commons
import qs.Ui

// uni-switch's icon in Omarchy's bar, put there by uni-switch (Settings → Bar icon).
// A click drops down its panel under the bar, where the click was; a right
// click opens its window. uni-switch is started if it isn't running.
BarWidget {
  id: root
  moduleName: "io.uni-switch.desktop"

  // written in by uni-switch when it puts the widget here
  readonly property string uniSwitch: "__UNI_SWITCH__"

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: String.fromCodePoint(0xf15c6) // nf-md-bird
    tooltipText: "uni-switch"
    onPressed: function(b) {
      Quickshell.execDetached(b === Qt.RightButton ? [root.uniSwitch, "app"] : [root.uniSwitch, "panel"])
    }
  }
}
