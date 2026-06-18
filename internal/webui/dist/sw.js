const CACHE_NAME="aura-precache-b4d162385422e822";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-BGCRezdI.js","revision":"e78c7bcd60e82ed3"},{"url":"/assets/authConfig-Ck3ILZ05.js","revision":"032f09c765f1626f"},{"url":"/assets/ExternalStoreChat-Bxl-xqw6.js","revision":"f65f1fcde1f41b3a"},{"url":"/assets/index-dsk5Vvvt.js","revision":"61d38284451769c5"},{"url":"/assets/index-65a8c0ef.css","revision":"75a6e115d282b92a"},{"url":"/assets/LoginPage-UZg_T-1e.js","revision":"21bfc326a902a4cc"},{"url":"/assets/NotFoundView-B_OnQMl9.js","revision":"bf73144db870f7b4"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/shiki-BM9IzjFr.js","revision":"e31a1a5e2ea0700e"},{"url":"/assets/useConversations-Bq_kkun0.js","revision":"7d61d0c6f327421c"},{"url":"/assets/useTranslation-BLQrOiQG.js","revision":"1ab57970e10ace57"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
self.addEventListener('install',(event)=>{
  event.waitUntil(caches.open(CACHE_NAME).then((cache)=>Promise.all(PRECACHE.map((entry)=>cache.add(entry.url).catch(()=>undefined)))).then(()=>self.skipWaiting()));
});
self.addEventListener('activate',(event)=>{
  event.waitUntil(caches.keys().then((keys)=>Promise.all(keys.filter((key)=>key.startsWith('aura-precache-')&&key!==CACHE_NAME).map((key)=>caches.delete(key)))).then(()=>self.clients.claim()));
});
self.addEventListener('fetch',(event)=>{
  const request=event.request;
  if(request.method!=='GET') return;
  const url=new URL(request.url);
  if(url.origin!==self.location.origin) return;
  if(request.mode==='navigate'){
    event.respondWith(fetch(request).catch(()=>caches.match('/index.html')));
    return;
  }
  event.respondWith(caches.match(request).then((cached)=>cached||fetch(request)));
});
