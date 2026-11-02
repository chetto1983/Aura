// A Dart client on the real AG-UI stream, cut on purpose. Results and scope:
// docs/superpowers/verification/2026-11-02-mobile-client-spike.md
//
//   docker run --rm -v "$PWD:/spike:ro" -v "$WORK:/work:ro" -e BASE=https://<host> \
//     -e COOKIE_FILE=/work/cookie dart:stable dart /spike/sse_spike.dart
//
// COOKIE_FILE holds a Cookie header value; the conversations it opens are deleted at the end.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

const count = 300;
const prompt =
    'Scrivi in un solo messaggio tutti i numeri interi da 1 a $count in ordine crescente, '
    'separati da uno spazio. Nessun altro testo e nessuno strumento.';

final base = Platform.environment['BASE']!;
final cookie = File(Platform.environment['COOKIE_FILE']!).readAsStringSync().trim();
final vmHost = Uri.parse(base).host;
final rng = Random.secure();

HttpClient newClient() => HttpClient()
  ..connectionTimeout = const Duration(seconds: 15)
  ..badCertificateCallback = (cert, host, port) => host == vmHost;

String uuid() {
  final b = List<int>.generate(16, (_) => rng.nextInt(256));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  final h = b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();
  return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-${h.substring(16, 20)}-${h.substring(20)}';
}

Future<HttpClientRequest> open(HttpClient c, String method, String path, Map<String, String> headers) async {
  final req = await c.openUrl(method, Uri.parse('$base$path'));
  req.followRedirects = false;
  req.headers.set('Cookie', cookie);
  req.headers.set('Origin', base);
  headers.forEach((k, v) => req.headers.set(k, v));
  return req;
}

Future<(int, String)> call(String method, String path, {Object? body, bool idem = false}) async {
  final c = newClient();
  try {
    final req = await open(c, method, path, {
      'Accept': 'application/json',
      if (idem) 'Idempotency-Key': uuid(),
      if (body != null) 'Content-Type': 'application/json',
    });
    if (body != null) req.write(jsonEncode(body));
    final res = await req.close();
    return (res.statusCode, await res.transform(utf8.decoder).join());
  } finally {
    c.close(force: true);
  }
}

class Frame {
  Frame(this.id, this.data);
  final int? id;
  final Map<String, dynamic> data;
  String get type => data['type'] as String? ?? '?';
}

Stream<Frame> frames(Stream<List<int>> body, void Function() onComment) async* {
  int? id;
  final data = <String>[];
  await for (final line in body.transform(utf8.decoder).transform(const LineSplitter())) {
    if (line.isEmpty) {
      if (data.isNotEmpty) yield Frame(id, jsonDecode(data.join('\n')) as Map<String, dynamic>);
      id = null;
      data.clear();
    } else if (line.startsWith(':')) {
      onComment();
    } else if (line.startsWith('id:')) {
      id = int.tryParse(line.substring(3).trim());
    } else if (line.startsWith('data:')) {
      data.add(line.substring(5).trimLeft());
    }
  }
}

class Turn {
  final text = StringBuffer();
  final types = <String, int>{};
  int frames = 0, textFrames = 0, gaps = 0, comments = 0;
  int? last;
  String? runId;
  bool terminal = false;

  void fold(Frame f) {
    frames++;
    types[f.type] = (types[f.type] ?? 0) + 1;
    final id = f.id;
    if (id != null) {
      if (last != null && id != last! + 1) gaps++;
      last = id;
    }
    if (f.type == 'RUN_STARTED') runId ??= f.data['runId'] as String?;
    if (f.type == 'TEXT_MESSAGE_CONTENT') {
      text.write(f.data['delta']);
      textFrames++;
    }
    if (f.type == 'RUN_FINISHED' || f.type == 'RUN_ERROR') terminal = true;
  }

  bool get intact {
    final nums = RegExp(r'\d+').allMatches(text.toString()).map((m) => int.parse(m[0]!)).toList();
    if (nums.length != count) return false;
    for (var i = 0; i < count; i++) {
      if (nums[i] != i + 1) return false;
    }
    return true;
  }
}

Future<int> waitIdle(String conv) async {
  final sw = Stopwatch()..start();
  while (sw.elapsed < const Duration(seconds: 170)) {
    final (s, raw) = await call('GET', '/api/conversations/$conv');
    if (s == 200) {
      final live = (jsonDecode(raw) as Map<String, dynamic>)['live_run_id'];
      if (live == null || live == '') return sw.elapsedMilliseconds;
    }
    await Future<void>.delayed(const Duration(milliseconds: 500));
  }
  return -1;
}

Future<void> scenario(String name, String conv, {int? cutAfter, Future<void> Function()? gap, bool attach = false}) async {
  final sw = Stopwatch()..start();
  final c = newClient();
  var t = Turn();
  final req = await open(c, 'POST', '/agent/run', {
    'Accept': 'text/event-stream',
    'Content-Type': 'application/json',
    'Idempotency-Key': uuid(),
  });
  req.write(jsonEncode({
    'threadId': conv,
    'runId': uuid(),
    'messages': [
      {'id': uuid(), 'role': 'user', 'content': prompt}
    ],
  }));
  final res = await req.close();
  final tHeaders = sw.elapsedMilliseconds;
  print('MEASURE $name headers: status=${res.statusCode} ct=${res.headers.contentType} '
      'cache=${res.headers.value("cache-control")} accel=${res.headers.value("x-accel-buffering")} '
      'chunked=${res.headers.chunkedTransferEncoding} t=${tHeaders}ms');
  if (res.statusCode != 200) {
    print('MEASURE $name body: ${await res.transform(utf8.decoder).join()}');
    return;
  }
  int? tFirst;
  var cut = false;
  await for (final f in frames(res, () => t.comments++)) {
    t.fold(f);
    if (f.type == 'TEXT_MESSAGE_CONTENT') tFirst ??= sw.elapsedMilliseconds;
    if (cutAfter != null && t.frames >= cutAfter) {
      cut = true;
      break;
    }
  }
  c.close(force: true);
  if (cut) {
    final cutAt = sw.elapsedMilliseconds;
    final cursor = t.last;
    final runId = t.runId;
    final partial = t.text.length;
    final framesBefore = t.frames;
    await gap!();
    final tGap = sw.elapsedMilliseconds - cutAt;
    var liveRun = '';
    if (attach) {
      final (_, dto) = await call('GET', '/api/conversations/$conv');
      liveRun = (jsonDecode(dto) as Map<String, dynamic>)['live_run_id'] as String? ?? '';
      if (liveRun.isEmpty) {
        print('MEASURE $name: live_run_id empty after ${tGap}ms, nothing to attach to');
        await waitIdle(conv);
        return;
      }
      t = Turn();
    }
    final c2 = newClient();
    final rq = await open(c2, 'GET', '/agent/runs/${attach ? liveRun : runId}/events', {
      'Accept': 'text/event-stream',
      if (!attach) 'Last-Event-ID': '$cursor',
    });
    final rs = await rq.close();
    final tResume = sw.elapsedMilliseconds;
    int? firstId;
    if (rs.statusCode == 200) {
      await for (final f in frames(rs, () => t.comments++)) {
        firstId ??= f.id;
        t.fold(f);
      }
    } else {
      await rs.drain<void>();
    }
    c2.close(force: true);
    print('MEASURE $name resume: cutAt=${cutAt}ms framesBefore=$framesBefore partialChars=$partial gap=${tGap}ms '
        'status=${rs.statusCode} cursor=$cursor firstId=$firstId liveRunMatches=${attach ? liveRun == runId : "n/a"} '
        'replay=${sw.elapsedMilliseconds - tResume}ms');
  }
  final idle = await waitIdle(conv);
  final (ss, raw) = await call('GET', '/threads/$conv/messages');
  final escaped = jsonEncode(t.text.toString());
  final verbatim = raw.contains(escaped.substring(1, escaped.length - 1));
  print('MEASURE $name result: firstText=${tFirst}ms total=${sw.elapsedMilliseconds}ms frames=${t.frames} '
      'textFrames=${t.textFrames} chars=${t.text.length} idGaps=${t.gaps} comments=${t.comments} '
      'terminal=${t.terminal} sequenceIntact=${t.intact} snapshot=$ss snapshotHasStreamedText=$verbatim '
      'tailsInSnapshot=${"${count - 1} $count".allMatches(raw).length} idleAfter=${idle}ms types=${t.types}');
}

Future<void> main() async {
  Future<String> newConversation() async {
    final (s, raw) = await call('POST', '/api/conversations', body: <String, Object?>{}, idem: true);
    if (s != 201) {
      print('MEASURE setup failed: $s $raw');
      exit(1);
    }
    return (jsonDecode(raw) as Map<String, dynamic>)['ID'] as String;
  }

  final live = await newConversation();
  final past = await newConversation();
  print('MEASURE conversations created');
  try {
    await scenario('F cut at RUN_STARTED, live resume after 1 s', live, cutAfter: 1, gap: () => Future<void>.delayed(const Duration(seconds: 1)));
    await scenario('G cut at RUN_STARTED, cold attach from live_run_id', live, cutAfter: 1, attach: true, gap: () => Future<void>.delayed(const Duration(seconds: 1)));
    await scenario('H cut at RUN_STARTED, fresh conversation, past the linger', past, cutAfter: 1, gap: () async {
      await waitIdle(past);
      await Future<void>.delayed(const Duration(seconds: 215));
    });
  } finally {
    for (final conv in [live, past]) {
      final (d, _) = await call('DELETE', '/api/conversations/$conv', idem: true);
      print('MEASURE conversation deleted: status=$d');
    }
  }
}
