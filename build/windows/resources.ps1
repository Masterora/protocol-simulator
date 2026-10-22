param([Parameter(Mandatory=$true)][string]$Executable)
$ErrorActionPreference = 'Stop'
if (-not ('ProtocolSimulatorResources' -as [type])) {
Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Text;
using System.Runtime.InteropServices;
using System.ComponentModel;
public static class ProtocolSimulatorResources {
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern IntPtr BeginUpdateResource(string file, bool delete);
    [DllImport("kernel32.dll", SetLastError=true)]
    static extern bool UpdateResource(IntPtr handle, IntPtr type, IntPtr name, ushort lang, byte[] data, uint length);
    [DllImport("kernel32.dll", SetLastError=true)]
    static extern bool EndUpdateResource(IntPtr handle, bool discard);
    static void Put(IntPtr h, int type, int name, byte[] b) {
        if (!UpdateResource(h,(IntPtr)type,(IntPtr)name,1033,b,(uint)b.Length)) throw new Win32Exception();
    }
    static void Align(BinaryWriter w) { while(w.BaseStream.Position%4!=0) w.Write((byte)0); }
    static byte[] Block(string key, byte[] value, bool text, params byte[][] children) {
        using(var m=new MemoryStream()) using(var w=new BinaryWriter(m)) {
            w.Write((ushort)0);w.Write((ushort)(text?value.Length/2:value.Length));w.Write((ushort)(text?1:0));
            w.Write(Encoding.Unicode.GetBytes(key+"\0"));Align(w);w.Write(value);
            foreach(var c in children) { Align(w);w.Write(c); }
            long size=m.Length;m.Position=0;w.Write((ushort)size);return m.ToArray();
        }
    }
    static byte[] Text(string k,string v) {return Block(k,Encoding.Unicode.GetBytes(v+"\0"),true);}
    static byte[] Version() {
        byte[] fixedInfo;
        using(var m=new MemoryStream()) using(var w=new BinaryWriter(m)) {
            foreach(uint n in new uint[]{0xFEEF04BD,0x10000,0x10000,0,0x10000,0,0x3F,0,0x40004,1,0,0,0})w.Write(n);
            fixedInfo=m.ToArray();
        }
        var strings=Block("StringFileInfo",new byte[0],true,Block("040904B0",new byte[0],true,
            Text("FileDescription","protocol-simulator"),Text("ProductName","protocol-simulator"),
            Text("InternalName","protocol-simulator"),Text("OriginalFilename","protocol-simulator.exe"),
            Text("FileVersion","1.0.0.0"),Text("ProductVersion","1.0.0.0")));
        var vars=Block("VarFileInfo",new byte[0],true,Block("Translation",new byte[]{9,4,176,4},false));
        return Block("VS_VERSION_INFO",fixedInfo,false,strings,vars);
    }
    public static void Apply(string exe,string ico,string manifest) {
        byte[] icon=File.ReadAllBytes(ico);
        IntPtr h=BeginUpdateResource(exe,false);if(h==IntPtr.Zero)throw new Win32Exception();
        bool committed=false;
        try {
            using(var r=new BinaryReader(new MemoryStream(icon)))
            using(var g=new MemoryStream()) using(var w=new BinaryWriter(g)) {
                if(r.ReadUInt16()!=0 || r.ReadUInt16()!=1)throw new InvalidDataException("Invalid ICO");
                ushort count=r.ReadUInt16();w.Write((ushort)0);w.Write((ushort)1);w.Write(count);
                for(int i=0;i<count;i++) {
                    byte[] dims=r.ReadBytes(8);uint size=r.ReadUInt32();uint offset=r.ReadUInt32();
                    byte[] image=new byte[size];Array.Copy(icon,offset,image,0,size);Put(h,3,i+1,image);
                    w.Write(dims);w.Write(size);w.Write((ushort)(i+1));
                }
                Put(h,14,1,g.ToArray());
                // Wails uses icon group resource 3 for native windows.
                Put(h,14,3,g.ToArray());
            }
            Put(h,16,1,Version());Put(h,24,1,File.ReadAllBytes(manifest));
            if(!EndUpdateResource(h,false))throw new Win32Exception();committed=true;
        } finally {if(!committed)EndUpdateResource(h,true);}
    }
}
'@
}
[ProtocolSimulatorResources]::Apply((Resolve-Path -LiteralPath $Executable).Path,(Join-Path $PSScriptRoot 'icon.ico'),(Join-Path $PSScriptRoot 'app.manifest'))
