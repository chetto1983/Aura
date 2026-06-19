const CACHE_NAME="aura-precache-bd33e62a63418ee7";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"5508e1ffcdd6c2eb"},{"url":"/assets/AppShell-2LKIVr5S.js","revision":"13d93e419072580f"},{"url":"/assets/authConfig-CGdgs8hk.js","revision":"55eec28e5629a471"},{"url":"/assets/ExternalStoreChat-BaWovE-X.js","revision":"5725d5a771aedc88"},{"url":"/assets/index-BS-xb1yy.js","revision":"e7c5ae73175bc7e4"},{"url":"/assets/index-e8c67f63.css","revision":"a9553122f9698096"},{"url":"/assets/LoginPage-AevCV-jm.js","revision":"ae4ab329f44113f3"},{"url":"/assets/NotFoundView-5n-yagAM.js","revision":"1c8744cab6260e67"},{"url":"/assets/rolldown-runtime-BUR9erT_.js","revision":"66bc2dc82ec39095"},{"url":"/assets/shiki-BM9IzjFr.js","revision":"e31a1a5e2ea0700e"},{"url":"/assets/useConversations-Bni-Ilji.js","revision":"280bf027b2f306e1"},{"url":"/assets/useTranslation-CZ0wjFaF.js","revision":"38682a3982e5835b"},{"url":"/favicon.svg","revision":"7b17af2ead77936a"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"3a814571ad551c73"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"c32d08b35f2cc4e2"},{"url":"/pwa-512.png","revision":"07720c285ef365f3"},{"url":"/pwa-maskable-512.png","revision":"33eae128615a06b6"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
