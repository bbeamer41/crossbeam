# MeshCore

Crossbeam can list and read its bulletins over a MeshCore companion radio. The articles stay in the same store as the packet prompt and the NomadNet pages. The radio is a serial device, the way a KISS modem is a separate program. Crossbeam does not become LoRa firmware, and it does not flash the radio.

MeshCore stays off until `mesh.device` is set. The value is a companion port such as `/dev/ttyACM0`, `/dev/ttyUSB0`, or a stable name under `/dev/serial/by-id/`. Leave it empty to keep the radio off. The path is read when `crossbeam serve` starts.

The radio has to be running companion firmware. Repeater firmware only forwards. Room-server firmware keeps its own chat history, and that history is not this bulletin list.

## Discovery

Crossbeam sets the companion radio's clock and its advert name when the port connects. The name is the station callsign. When the callsign is empty, the name is the station name, or BBS. It then sends one flood advert, so a MeshCore user can discover this station and message it the way they message another companion. Both radios have to have each other before a direct message can be read. The flood advert goes out again after three hours. It does not go out on every reconnect.

A chat advert that the companion radio did not store is saved, so this station can answer that contact. Repeater, room, and sensor adverts are not saved. Crossbeam does not change the radio's saved auto-add setting.

## Direct messages

Someone sends a direct message to this station. The reply comes back in that same conversation. A plain sentence gets one short reply that says how to list and read bulletins. The bulletin commands are the same as the packet prompt:

```text
K5LEB>
LIST
1  09-Nov  K5LEB  Net night notes
K5LEB>
READ 1
Title: Net night notes
Author: K5LEB

The Denton net meets on Thursday.
K5LEB>
BYE
Bye
```

`LIST` prints the store, newest first. The number is the row id. `READ` and that number print the packet bulletin. `?` lists the commands. `BYE` ends the reply. A message on a channel is ignored, and Crossbeam does not post a bulletin body on a channel.

A reply longer than 133 characters is split, and each piece starts with a mark such as `[1/3]`. At most 16 pieces are sent. The last piece says the rest of the bulletin is on the packet board. The article in the store is unchanged.

The companion link sends one frame at a time. Crossbeam does not read channel secrets, and it does not store them.

One station gets twelve answers a minute. The radio answers at most thirty direct messages a minute. A message past either cap is ignored until that minute has passed. A direct message longer than the MeshCore text limit is ignored, and so is a message that contains a zero byte.

## Service

The packaged service is in the `dialout` group, and it may open `ttyACM` and `ttyUSB` devices. The opened port has to be one of those USB serial devices. Crossbeam holds it exclusively while the port is open. Plug in the companion radio, set `mesh.device`, and restart the service. A missing radio is reported as `the MeshCore radio is not connected`. A port the service cannot open is reported as `permission denied`. A port this service already has open is reported as `the MeshCore radio is in use`. Those lines do not include the device path. Crossbeam dials the port again after it drops.

```bash
sudo -u crossbeam crossbeam settings set mesh.device /dev/ttyACM0 --state /var/lib/crossbeam
sudo systemctl restart crossbeam
```
