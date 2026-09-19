# 术语

## backup

独立 vault 数据类型：按 group + key 组织的加密条目，带 value 与 description。用于**不常用**的备份数据，与常用的 text 分开，避免混在同一浏览/操作体验里。

不是缓存：无 TTL、不因「可丢」而本机-only；与 text 一样是持久 vault 数据。

## text（对照）

常用加密文本块。backup 参考其字段、加密与 512KB 上限，但不复用 text 存储或分组。

## 512KB 上限

只计单条 value 的明文字节；description 与加密信封不计。超限拒绝写入，不截断。
