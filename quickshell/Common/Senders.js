.pragma library

// Thread sender-line formatting for the triage list (QML + Node testable):
// Gmail-style comma-separated participants with the latest sender emphasized,
// localized "me" for the user's own account, shortened earlier names, and the
// avatar chosen from the latest non-self participant when available.

function decodeBase64Utf8(b64) {
    var alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    var clean = String(b64).replace(/[^A-Za-z0-9+/=]/g, "");
    var bytes = [];
    for (var i = 0; i < clean.length; i += 4) {
        var c1 = alphabet.indexOf(clean.charAt(i));
        var c2 = alphabet.indexOf(clean.charAt(i + 1));
        var c3 = clean.charAt(i + 2) === "=" ? -1 : alphabet.indexOf(clean.charAt(i + 2));
        var c4 = clean.charAt(i + 3) === "=" ? -1 : alphabet.indexOf(clean.charAt(i + 3));
        if (c1 < 0 || c2 < 0)
            break;
        bytes.push((c1 << 2) | (c2 >> 4));
        if (c3 >= 0) {
            bytes.push(((c2 & 15) << 4) | (c3 >> 2));
            if (c4 >= 0)
                bytes.push(((c3 & 3) << 6) | c4);
        }
    }
    var hex = "";
    for (var j = 0; j < bytes.length; j++) {
        var h = bytes[j].toString(16);
        hex += "%" + (h.length < 2 ? "0" + h : h);
    }
    return decodeURIComponent(hex);
}

function decodeMimeWords(str) {
    if (!str)
        return "";
    return String(str).replace(/=\?([^?]+)\?([qQbB])\?([^?]*)\?=/g, function(match, _charset, enc, text) {
        var mode = enc.toUpperCase();
        if (mode === "Q") {
            var qText = text
                .replace(/%/g, "%25")
                .replace(/_/g, " ")
                .replace(/=([0-9A-Fa-f]{2})/g, function(_, hex) { return "%" + hex; });
            try {
                return decodeURIComponent(qText);
            } catch (e) {
                return text;
            }
        }
        if (mode === "B") {
            try {
                return decodeBase64Utf8(text);
            } catch (e) {
                return text;
            }
        }
        return match;
    });
}

function parseAddress(raw) {
    if (!raw)
        return { name: "", email: "" };
    var decoded = decodeMimeWords(raw);
    var m = decoded.match(/^\s*"?([^"<]*?)"?\s*<([^>]+)>\s*$/);
    if (m) {
        return {
            name: m[1].replace(/^"+|"+$/g, "").trim(),
            email: m[2].trim()
        };
    }
    var cleaned = decoded.replace(/^<|>$/g, "").trim();
    return { name: "", email: cleaned };
}

function displayName(raw) {
    var a = parseAddress(raw);
    return a.name !== "" ? a.name : a.email;
}

function isSelf(raw, accountEmail) {
    var acct = String(accountEmail || "").trim().toLowerCase();
    if (acct === "")
        return false;
    return parseAddress(raw).email.toLowerCase() === acct;
}

function shortName(raw, accountEmail, meLabel, maxLen) {
    var limit = maxLen || 14;
    if (isSelf(raw, accountEmail))
        return meLabel || "me";
    var name = displayName(raw);
    if (name === "")
        return "";
    var result = "";
    if (!/\s/.test(name) && name.indexOf("@") !== -1) {
        result = name.split("@")[0];
    } else {
        var parts = name.split(/\s+/);
        var tokens = [];
        for (var i = 0; i < parts.length; i++) {
            var tok = parts[i].replace(/^[,;"']+/, "").replace(/[,;"']+$/, "");
            if (tok !== "")
                tokens.push(tok);
        }
        if (tokens.length === 0)
            result = name;
        else if (tokens.length > 1 && (tokens[0].slice(-1) === "." || tokens[0].length <= 2))
            result = tokens[0] + " " + tokens[1];
        else
            result = tokens[0];
    }
    if (result.length > limit)
        return result.slice(0, Math.max(1, limit - 1)) + "\u2026";
    return result;
}

function participantKey(raw, accountEmail) {
    if (isSelf(raw, accountEmail))
        return "__self__";
    var a = parseAddress(raw);
    return (a.name || a.email).toLowerCase();
}

function dedupeOrdered(list, keyFn) {
    var out = [];
    var seen = {};
    for (var i = 0; i < list.length; i++) {
        var item = list[i];
        if (!item)
            continue;
        var k = keyFn(item);
        if (!k || seen[k])
            continue;
        seen[k] = true;
        out.push(item);
    }
    return out;
}

function formatThreadSenders(thread, accountEmail, meLabel, isSentView) {
    var me = meLabel || "me";
    var participants = (thread && Array.isArray(thread.participants)) ? thread.participants.filter(Boolean) : [];
    var lastSender = (thread && thread.lastSender) ? String(thread.lastSender) : "";
    var msgCount = (thread && thread.messageCount > 1) ? thread.messageCount : 0;

    if (isSentView) {
        var sentList = dedupeOrdered(participants, function(item) {
            var a = parseAddress(item);
            return (a.email || a.name).toLowerCase();
        });
        if (sentList.length === 0)
            return { prefix: "", latest: "", fullText: "", count: 0, avatarRaw: "", tooltip: "" };

        var sentPrefix = "";
        var sentLatest = displayName(sentList[sentList.length - 1]);
        if (sentList.length === 2)
            sentPrefix = shortName(sentList[0], "", me) + ", ";
        else if (sentList.length >= 3)
            sentPrefix = shortName(sentList[0], "", me) + ", \u2026, ";

        var sentTooltip = "";
        if (sentList.length > 1 || msgCount > 1) {
            var sentNames = sentList.map(displayName);
            sentTooltip = sentNames.join(", ") + (msgCount > 1 ? " (" + msgCount + ")" : "");
        }
        return {
            prefix: sentPrefix,
            latest: sentLatest,
            fullText: sentPrefix + sentLatest,
            count: msgCount,
            avatarRaw: sentList[0],
            tooltip: sentTooltip
        };
    }

    var ordered = dedupeOrdered(participants, function(item) {
        return participantKey(item, accountEmail);
    });
    var latestRaw = lastSender !== "" ? lastSender : (ordered.length > 0 ? ordered[ordered.length - 1] : "");
    if (latestRaw !== "") {
        var latestKey = participantKey(latestRaw, accountEmail);
        ordered = ordered.filter(function(item) {
            return participantKey(item, accountEmail) !== latestKey;
        });
        ordered.push(latestRaw);
    }

    if (ordered.length === 0) {
        return {
            prefix: "",
            latest: "",
            fullText: "",
            count: msgCount,
            avatarRaw: "",
            tooltip: ""
        };
    }

    var avatarRaw = ordered[ordered.length - 1];
    for (var i = ordered.length - 1; i >= 0; i--) {
        if (!isSelf(ordered[i], accountEmail)) {
            avatarRaw = ordered[i];
            break;
        }
    }

    var latestEntry = ordered[ordered.length - 1];
    var latestLabel = isSelf(latestEntry, accountEmail) ? me : displayName(latestEntry);
    var prefix = "";
    if (ordered.length === 2)
        prefix = shortName(ordered[0], accountEmail, me) + ", ";
    else if (ordered.length >= 3)
        prefix = shortName(ordered[0], accountEmail, me) + ", \u2026, ";

    var tooltip = "";
    if (ordered.length > 1 || msgCount > 1) {
        var chrono = dedupeOrdered(
            lastSender !== "" ? participants.concat([lastSender]) : participants,
            function(item) { return participantKey(item, accountEmail); }
        );
        var names = chrono.map(function(p) {
            return isSelf(p, accountEmail) ? me : displayName(p);
        });
        tooltip = names.join(", ") + (msgCount > 1 ? " (" + msgCount + ")" : "");
    }

    return {
        prefix: prefix,
        latest: latestLabel,
        fullText: prefix + latestLabel,
        count: msgCount,
        avatarRaw: avatarRaw,
        tooltip: tooltip
    };
}
