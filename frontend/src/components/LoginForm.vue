<script setup>
import { ref } from "vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { CircleAlertIcon as CircleAlert, RefreshCwIcon as RefreshCw } from "@lucide/vue";
import { useApp } from "@/state.js";

const s = useApp();
const username = ref("");
const password = ref("");
const errorMessage = ref("");
const loading = ref(false);

async function handleLogin() {
  if (!username.value.trim() || !password.value) {
    errorMessage.value = "请输入管理员用户名与密码";
    return;
  }
  loading.value = true;
  errorMessage.value = "";
  try {
    const ok = await s.login(username.value.trim(), password.value);
    if (!ok) {
      errorMessage.value = s.loginError?.value || "用户名或密码错误，请重试";
    }
  } catch (err) {
    errorMessage.value = err.message || "登录请求失败，请检查网络或配置";
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <Card class="w-full shadow-md border-border/80 bg-card">
    <CardHeader class="space-y-1 pb-4">
      <div class="flex items-center gap-2 mb-2">
        <div class="size-7 rounded-md bg-foreground text-background flex items-center justify-center font-bold text-xs select-none shadow-xs">
          P
        </div>
        <span class="text-xs font-semibold uppercase tracking-wider text-muted-foreground font-mono">
          PandaOPDS
        </span>
      </div>
      <CardTitle class="text-xl font-bold tracking-tight text-foreground">
        登录管理控制台
      </CardTitle>
      <CardDescription class="text-xs text-muted-foreground">
        输入管理员凭据（HTTP Basic Auth）以访问系统配置与服务仪表盘
      </CardDescription>
    </CardHeader>
    <CardContent>
      <form @submit.prevent="handleLogin" class="space-y-4">
        <Alert v-if="errorMessage" variant="destructive" class="py-2.5 px-3">
          <CircleAlert class="size-4 shrink-0 text-destructive" />
          <AlertDescription class="text-xs">{{ errorMessage }}</AlertDescription>
        </Alert>

        <div class="space-y-1.5">
          <label class="text-xs font-medium text-foreground leading-none">
            用户名
          </label>
          <Input
            v-model="username"
            type="text"
            autocomplete="username"
            placeholder="admin"
            autofocus
            required
            :disabled="loading"
          />
        </div>

        <div class="space-y-1.5">
          <div class="flex items-center justify-between">
            <label class="text-xs font-medium text-foreground leading-none">
              密码
            </label>
          </div>
          <Input
            v-model="password"
            type="password"
            autocomplete="current-password"
            placeholder="••••••••"
            required
            :disabled="loading"
          />
        </div>

        <Button
          type="submit"
          class="w-full h-9 font-medium text-sm mt-2"
          :disabled="loading"
        >
          <RefreshCw v-if="loading" class="size-3.5 mr-2 animate-spin" />
          {{ loading ? "验证中..." : "登录" }}
        </Button>
      </form>
    </CardContent>
    <CardFooter class="pt-0 pb-4 justify-center">
      <p class="text-[11px] text-muted-foreground">
        受保护的本地服务 · 凭据将安全保存在本地浏览器会话中
      </p>
    </CardFooter>
  </Card>
</template>
