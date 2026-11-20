const fs=require('node:fs');const path=require('node:path');const cp=require('node:child_process');
const root=path.resolve('public-android-tool');
function git(args){return cp.execFileSync('git',['-C',root,...args],{encoding:'utf8'}).trim();}
if(git(['rev-parse','HEAD'])!=='a421e43855164a8197daf9d8d40fe71c6996bb0d'||git(['status','--porcelain','--untracked-files=all']))throw Error('public bundle authority');
for(const name of ['sdk-installer','emulator-manager'])require(path.join(root,'lib',name+'.js'));
require(path.join(root,'node_modules/@actions/exec'));
if(git(['status','--porcelain','--untracked-files=all']))throw Error('public bundle mutation');
console.log('STAGE android-bundle PASS');
