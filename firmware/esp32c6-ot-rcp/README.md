# ReefTank ESP32-C6 OpenThread RCP

This is the official ESP-IDF `examples/openthread/ot_rcp` example, pinned in
the Hub repository and configured for the native USB Serial/JTAG connector on
the user's MuseLab nanoESP32-C6. The C6 is a radio co-processor only: the
Raspberry Pi runs OTBR and Home Assistant runs the Matter controller.

Build with ESP-IDF 6.1 (the same version used by the release workflow):

```powershell
. D:\HomeAssistant\esp-idf\export.ps1
idf.py set-target esp32c6
idf.py build
idf.py merge-bin -o build/reeftank-esp32c6-ot-rcp.bin
```

The first flash must be performed over USB while OTBR is stopped. Future RCP
updates are also applied over the same USB link by the privileged Hub
maintenance path; they are never sent through Thread or Wi-Fi.

The USB Spinel transport intentionally avoids Wi-Fi/Thread radio coexistence
on the C6. The Pi supplies Ethernet backhaul and stores the Thread dataset.
