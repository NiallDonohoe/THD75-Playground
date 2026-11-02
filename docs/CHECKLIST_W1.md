# Checklist W1 — the d75winlink page in demo mode

No radio and no Winlink account are needed. The program runs a fake Winlink
server inside itself, with made-up mail from callsigns starting with Q and
addresses at example.com. Nothing leaves the Mac: messages you "send" go
to the fake server, which answers each one.

## Run it

```sh
make build
./bin/d75winlink --demo --call YOURCALL
```

Use your own callsign. The demo mailbox is kept apart from real mail, in
`~/Library/Application Support/d75winlink/demo/`. Delete that folder to
start the demo afresh. Then open http://127.0.0.1:8078/ in Chrome.

## Check

1. **Status bar**: your callsign, "Demo", "RF transmit off", and "live" in
   green.
2. **First session**: press **Connect**. The session log fills: the login,
   the handshake (your password's answer shows as `;PR: ********`), three
   offers from the server and their transfer, a progress bar for each, then
   "Done: 0 messages sent, 3 received". The link is slowed to a
   radio-like pace, so this takes a few seconds.
3. **Inbox**: three unread messages (blue dots, a count on Inbox): a net
   reminder from Q4NET, a reply from friend@example.com with an attachment
   (📎), and exercise traffic from Q7EOC with a Cc.
4. **Reading**: open each. The dot and the count go as you read. The
   friend's message has `site-map.txt`: click it and it downloads (it never
   opens in the page). Check From, To, Cc and Date look right.
5. **Reply**: reply to the friend's message. To and "Re:" are filled in,
   with the original quoted. Type a line with an emoji: a red note says
   Winlink cannot carry it. Remove it: the size and the airtime at 1200 and
   9600 bps appear, and a note about Winlink's whitelist for internet
   replies.
6. **Draft**: reload the page with the compose form open. Press Compose:
   your draft is still there.
7. **Compose**: write a new message to two recipients at once, one callsign
   and one email address (`Q2XYZ, you@example.com`), and attach a small
   file. Try an address the page cannot read (`someone@nowhere`): the To
   box turns red with the reason.
8. **Outbox**: Put in Outbox. Each message shows its size, its compressed
   size on the air, and a rough time at 1200 bps; the session panel adds
   them up. Open one: **Delete (unsend)** asks for a second click.
9. **Second session**: Connect. Your messages go out, each with a progress
   bar, and move to Sent ("1 message sent" or more). The three demo
   messages are offered again and refused (`FS ---`), because you already
   have them.
10. **Replies**: Connect once more. A reply to each message you sent
    arrives ("Re: …", from whoever you sent it to, saying it is an
    automatic reply from the demo server).
11. **Stop**: queue something long (paste a page of text), Connect and
    press **Stop** while it is sending. The session says "Stopped" and the
    message stays in the Outbox. Connect again and it goes.
12. **Archive**: archive a message from the Inbox, move it back, archive it
    again and delete it from the Archive.
13. **Restart**: quit with Ctrl-C and start the program again. The page says
    "reconnecting…", then carries on by itself. Your folders, and which
    messages are read, are as you left them.
14. **Appearance**: macOS light and dark both read well.

## Send back

- Anything that looked wrong or confusing, especially the session log, the
  size and airtime figures, and the notes under the compose form.
- Whether the three-pane layout suits you, or you would rather have
  something else.
- Whether you have a Winlink account for your callsign yet, for W2.
