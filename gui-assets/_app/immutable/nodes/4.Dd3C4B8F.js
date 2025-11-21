import{c as C,a as r,d as te,f as b,t as J,s as ae}from"../chunks/BOaHAOf9.js";import{f as R,z as re,B as se,F as i,C as t,c as p,I as U,H as D,G as e,h as oe,J as u,k as de}from"../chunks/CYtTFtD_.js";import{i as A,a as ie,s as ne}from"../chunks/BE3f0_xd.js";import{e as le,i as ce,u as E}from"../chunks/DFyFtZMB.js";import{I as ve,s as me,a as pe}from"../chunks/BrH4xPQt.js";import{g as ue}from"../chunks/Ca1nNKxN.js";import{I as fe,B as O}from"../chunks/Gph-JeDs.js";import"../chunks/Cj1P4Vg3.js";import{l as xe,s as he}from"../chunks/4o4y3d7k.js";import{P as F}from"../chunks/BNoi57Bw.js";function K(W,k){const N=xe(k,["children","$$slots","$$events","$$legacy"]);/**
 * @license lucide-svelte v0.554.0 - ISC
 *
 * ISC License
 *
 * Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2023 as part of Feather (MIT). All other copyright (c) for Lucide are held by Lucide Contributors 2025.
 *
 * Permission to use, copy, modify, and/or distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 *
 * ---
 *
 * The MIT License (MIT) (for portions derived from Feather)
 *
 * Copyright (c) 2013-2023 Cole Bemis
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 *
 */const B=[["path",{d:"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"}]];ve(W,he({name:"shield"},()=>N,{get iconNode(){return B},children:(G,l)=>{var f=C(),y=R(f);me(y,k,"default",{}),r(G,f)},$$slots:{default:!0}}))}var ge=b('<div class="text-center space-y-8"><h1 class="text-4xl font-bold text-primary">Welcome to Arkham</h1> <p class="text-2xl text-foreground">The Decentralized Private Network (dPN)</p> <div class="space-y-4 max-w-md mx-auto"><!> <div class="text-sm text-muted-foreground"> </div></div> <!></div>'),_e=b('<div class="text-center space-y-8"><h2 class="text-3xl font-bold">How Arkham Works</h2> <div class="grid md:grid-cols-2 gap-6 mt-8"><div class="bg-card p-6 rounded-lg border border-border"><!> <h3 class="text-xl font-semibold mb-2">Pay As You Go</h3> <p class="text-muted-foreground">Use the dVPN and pay only for what you consume with crypto</p></div> <div class="bg-card p-6 rounded-lg border border-border"><!> <h3 class="text-xl font-semibold mb-2">Earn Crypto</h3> <p class="text-muted-foreground">Provide bandwidth and earn rewards in SOL and $ARKHAM</p></div></div> <!></div>'),be=b('<div class="text-center space-y-8"><h2 class="text-3xl font-bold">Ready to Get Started?</h2> <p class="text-xl text-muted-foreground">Join the decentralized revolution and take control of your privacy</p> <div class="flex justify-center gap-4"><!></div></div>'),ye=b('<div class="text-center space-y-8"><h2 class="text-3xl font-bold">Choose Your Role</h2> <p class="text-muted-foreground">You can always switch later in settings</p> <div class="grid md:grid-cols-2 gap-6 mt-8"><button class="bg-card p-8 rounded-lg border-2 border-border hover:border-primary transition-colors text-left"><!> <h3 class="text-2xl font-semibold mb-2">Become a Seeker</h3> <p class="text-muted-foreground">I want to use the dVPN and protect my privacy</p></button> <button class="bg-card p-8 rounded-lg border-2 border-border hover:border-primary transition-colors text-left"><!> <h3 class="text-2xl font-semibold mb-2">Become a Warden</h3> <p class="text-muted-foreground">I want to provide bandwidth and earn crypto</p></button></div></div>'),we=b("<div></div>"),$e=b('<div class="min-h-screen flex items-center justify-center p-4"><div class="w-full max-w-2xl"><!> <div class="flex justify-center gap-2 mt-8"></div></div></div>');function Be(W,k){re(k,!0);const N=()=>ne(E,"$userStore",B),[B,G]=ie();let l=U(1),f=U("");const y=["AnonymousUser","GhostInTheMachine","Cipher","ShadowRunner","PhantomNode","CryptoNomad"],q=y[Math.floor(Math.random()*y.length)];function M(){p(l)<4&&de(l)}function V(a){const d=p(f).trim()||q,s={hasOnboarded:!0,role:a,nickname:d,selectedProfile:a,isWardenRegistered:a==="warden"?N().isWardenRegistered:!1,isWardenActive:a==="warden"?N().isWardenActive:!1};localStorage.setItem("arkham_user",JSON.stringify(s)),E.set(s),ue(a==="warden"?"/warden":"/seeker",{replaceState:!0})}var j=$e(),Y=t(j),L=t(Y);{var Q=a=>{var d=ge(),s=i(t(d),4),P=t(s);fe(P,{type:"text",placeholder:"Choose a nickname",class:"text-lg",get value(){return p(f)},set value(o){oe(f,o,!0)}});var S=i(P,2),c=t(S);e(S),e(s);var v=i(s,2);O(v,{onclick:M,size:"lg",class:"px-8",children:(o,x)=>{u();var w=J("Next");r(o,w)},$$slots:{default:!0}}),e(d),D(o=>ae(c,`Suggested: ${o??""}`),[()=>y.join(", ")]),r(a,d)},X=a=>{var d=C(),s=R(d);{var P=c=>{var v=_e(),o=i(t(v),2),x=t(o),w=t(x);K(w,{class:"w-12 h-12 text-primary mx-auto mb-4"}),u(4),e(x);var n=i(x,2),m=t(n);F(m,{class:"w-12 h-12 text-primary mx-auto mb-4"}),u(4),e(n),e(o);var h=i(o,2);O(h,{onclick:M,size:"lg",class:"px-8",children:($,g)=>{u();var _=J("Next");r($,_)},$$slots:{default:!0}}),e(v),r(c,v)},S=c=>{var v=C(),o=R(v);{var x=n=>{var m=be(),h=i(t(m),4),$=t(h);O($,{onclick:M,size:"lg",class:"px-8",children:(g,_)=>{u();var I=J("Let's Go!");r(g,I)},$$slots:{default:!0}}),e(h),e(m),r(n,m)},w=n=>{var m=C(),h=R(m);{var $=g=>{var _=ye(),I=i(t(_),4),z=t(I);z.__click=()=>V("seeker");var Z=t(z);K(Z,{class:"w-16 h-16 text-primary mb-4"}),u(4),e(z);var H=i(z,2);H.__click=()=>V("warden");var ee=t(H);F(ee,{class:"w-16 h-16 text-primary mb-4"}),u(4),e(H),e(I),e(_),r(g,_)};A(h,g=>{p(l)===4&&g($)},!0)}r(n,m)};A(o,n=>{p(l)===3?n(x):n(w,!1)},!0)}r(c,v)};A(s,c=>{p(l)===2?c(P):c(S,!1)},!0)}r(a,d)};A(L,a=>{p(l)===1?a(Q):a(X,!1)})}var T=i(L,2);le(T,20,()=>[1,2,3,4],ce,(a,d)=>{var s=we();D(()=>pe(s,1,`w-2 h-2 rounded-full ${p(l)===d?"bg-primary":"bg-muted"}`)),r(a,s)}),e(T),e(Y),e(j),r(W,j),se(),G()}te(["click"]);export{Be as component};
