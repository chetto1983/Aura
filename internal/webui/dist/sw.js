const CACHE_NAME="aura-precache-e6b0331a6f25ff63";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"1b368f466faf5113"},{"url":"/assets/AppShell-Djsa_Cca.js","revision":"3ab69e26e6e64e3f"},{"url":"/assets/index-64mhLMLx.js","revision":"f48bffaa43dcfe16"},{"url":"/assets/index-BU2BlgPZ.css","revision":"a60542ebd5e891d4"},{"url":"/assets/LanguageSwitcher-Bl3icDeY.js","revision":"30f99edcf1f0ec47"},{"url":"/assets/LoginPage-Dlkazj9s.js","revision":"a23699bbac91bee0"},{"url":"/assets/NotFoundView-BbCwHw3p.js","revision":"c366a1fe03ba4062"},{"url":"/assets/useTranslation-BHhjhjE0.js","revision":"a289af906795965b"},{"url":"/favicon.svg","revision":"b4e1473c65c90bf8"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"10a648d242ed05ba"},{"url":"/manifest.webmanifest","revision":"56e207c40260c0da"},{"url":"/pwa-192.png","revision":"8fad95a5f7364b80"},{"url":"/pwa-512.png","revision":"f70794c5678d3cd1"},{"url":"/pwa-maskable-512.png","revision":"f5d9b3da931806c1"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
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
