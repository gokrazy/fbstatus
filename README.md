# gokrazy framebuffer status (fbstatus)

fbstatus graphically shows the gokrazy system status on the Linux frame buffer,
which is typically available via HDMI when running on a Raspberry Pi or a PC.

![fbstatus screenshot](https://github.com/gokrazy/fbstatus/blob/main/2021-08-30-fbstatus-screenshot.png?raw=true)

This graphical output is helpful to understand:

1. whether gokrazy booted up correctly
1. whether gokrazy’s NTP client obtained the correct time
1. whether gokrazy’s DHCP client obtained an IP address
1. the current resource usage of the system

## Usage

Please refer to [the gokrazy quickstart](https://gokrazy.org/quickstart/) if you are unfamiliar.

The `gok new` command already adds the `fbstatus` package by default, so no
further customization is needed. If you have removed the package for some
reason, you can always re-add it with:

```
gok add github.com/gokrazy/fbstatus
```

After your Raspberry Pi reboots, you should eventually see the graphical output
from the screenshot above on your HDMI monitor.

## TODO

* show ethernet interface(s) plugged-in state somehow?
* show service log messages (stdout, stderr)
* implement different views, switch between the different views via flag (rotate mode, switches every n=60 seconds between views)
