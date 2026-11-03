#!/usr/bin/env python3
"""Make Discord show you playing "gdb" via Rich Presence.

Usage: DISCORD_CLIENT_ID=<app-id> python3 discord-gdb.py [details] [state]
Get an app id at https://discord.com/developers/applications (name the app "gdb").
"""
import json
import os
import socket
import struct
import sys
import time
import uuid

CLIENT_ID = os.environ.get("DISCORD_CLIENT_ID", "")


def find_ipc_socket():
    bases = [os.environ.get("XDG_RUNTIME_DIR"), os.environ.get("TMPDIR"), "/tmp"]
    for base in bases:
        if not base:
            continue
        for i in range(10):
            path = os.path.join(base, f"discord-ipc-{i}")
            if os.path.exists(path):
                return path
    return None


def send_frame(sock, op, payload):
    data = json.dumps(payload).encode()
    sock.sendall(struct.pack("<II", op, len(data)) + data)


def recv_frame(sock):
    header = sock.recv(8)
    if len(header) < 8:
        return None, None
    op, length = struct.unpack("<II", header)
    data = b""
    while len(data) < length:
        chunk = sock.recv(length - len(data))
        if not chunk:
            break
        data += chunk
    return op, json.loads(data)


def set_activity(sock, activity):
    send_frame(sock, 1, {
        "cmd": "SET_ACTIVITY",
        "args": {"pid": os.getpid(), "activity": activity},
        "nonce": str(uuid.uuid4()),
    })
    _, resp = recv_frame(sock)
    return resp


def main():
    if not CLIENT_ID:
        sys.exit("Set DISCORD_CLIENT_ID (see docstring).")
    details = sys.argv[1] if len(sys.argv) > 1 else "Debugging"
    state = sys.argv[2] if len(sys.argv) > 2 else "(gdb) run"

    path = find_ipc_socket()
    if not path:
        sys.exit("No discord-ipc socket found — is the Discord desktop app running?")

    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.connect(path)

    send_frame(sock, 0, {"v": 1, "client_id": CLIENT_ID})
    _, resp = recv_frame(sock)
    if resp.get("evt") != "READY":
        sys.exit(f"Handshake failed (check the client id): {resp}")

    resp = set_activity(sock, {
        "type": 0,  # Playing
        "details": details,
        "state": state,
        "timestamps": {"start": int(time.time())},
        "instance": False,
    })
    if resp.get("evt") == "ERROR":
        sys.exit(f"SET_ACTIVITY failed: {resp}")

    print(f"Discord now shows you playing gdb — '{details}' / '{state}'.")
    print("Ctrl+C to clear and quit.")
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        set_activity(sock, None)
        print("\nPresence cleared.")


if __name__ == "__main__":
    main()
