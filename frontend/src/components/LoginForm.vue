<script setup>
import { ref } from "vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
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
const showForgotTip = ref(false);
const showOAuthTip = ref(false);

async function handleLogin() {
  if (!username.value.trim() || !password.value) {
    errorMessage.value = "Please enter your email/username and password.";
    return;
  }
  loading.value = true;
  errorMessage.value = "";
  try {
    const ok = await s.login(username.value.trim(), password.value);
    if (!ok) {
      errorMessage.value = s.loginError?.value || "Invalid credentials. Please try again.";
    }
  } catch (err) {
    errorMessage.value = err.message || "Failed to sign in. Please verify network or server config.";
  } finally {
    loading.value = false;
  }
}

function handleOAuthClick() {
  showOAuthTip.value = !showOAuthTip.value;
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card class="rounded-xl border border-border bg-card text-card-foreground shadow-sm py-0 gap-0">
      <CardHeader class="flex flex-col gap-1.5 p-6 pb-4">
        <CardTitle class="text-2xl font-bold tracking-tight text-foreground">
          Login to your account
        </CardTitle>
        <CardDescription class="text-sm text-muted-foreground mt-1.5">
          Enter your email below to login to your account
        </CardDescription>
      </CardHeader>
      <CardContent class="p-6 pt-0">
        <form @submit.prevent="handleLogin">
          <div class="flex flex-col gap-6">
            <Alert v-if="errorMessage" variant="destructive" class="py-2.5 px-3">
              <CircleAlert class="size-4 shrink-0 text-destructive" />
              <AlertDescription class="text-xs">{{ errorMessage }}</AlertDescription>
            </Alert>

            <div class="grid gap-2">
              <label for="email" class="text-sm font-medium leading-none text-foreground select-none">
                Email
              </label>
              <Input
                id="email"
                v-model="username"
                type="text"
                autocomplete="username"
                placeholder="m@example.com"
                required
                :disabled="loading"
                class="h-9"
              />
            </div>

            <div class="grid gap-2">
              <div class="flex items-center">
                <label for="password" class="text-sm font-medium leading-none text-foreground select-none">
                  Password
                </label>
                <a
                  href="#"
                  class="ml-auto inline-block text-sm text-muted-foreground underline-offset-4 hover:underline hover:text-foreground cursor-pointer select-none"
                  @click.prevent="showForgotTip = !showForgotTip"
                >
                  Forgot your password?
                </a>
              </div>
              <Input
                id="password"
                v-model="password"
                type="password"
                autocomplete="current-password"
                required
                :disabled="loading"
                class="h-9"
              />
              <p v-if="showForgotTip" class="text-xs text-muted-foreground mt-1 bg-muted/60 p-2.5 rounded-md border border-border leading-relaxed">
                💡 管理员凭据由部署环境变量 <code>ADMIN_USER</code> 和 <code>ADMIN_PASS</code> 控制。若遗忘请在部署环境检查或修改配置。
              </p>
            </div>

            <Button
              type="submit"
              class="w-full h-9 font-medium text-sm shadow-xs"
              :disabled="loading"
            >
              <RefreshCw v-if="loading" class="size-3.5 mr-2 animate-spin" />
              {{ loading ? "Signing in..." : "Login" }}
            </Button>

            <Button
              type="button"
              variant="outline"
              class="w-full h-9 font-medium text-sm shadow-xs -mt-3"
              @click="handleOAuthClick"
            >
              Login with Google
            </Button>

            <p v-if="showOAuthTip" class="text-xs text-muted-foreground -mt-3 bg-muted/60 p-2.5 rounded-md border border-border text-center leading-relaxed">
              Google 账户单点登录未启用。当前仅支持 HTTP Basic Auth 管理员凭据认证。
            </p>

            <div class="text-center text-sm text-muted-foreground">
              Don't have an account?
              <a
                href="https://github.com/chock/PandaOPDS#configuration"
                target="_blank"
                rel="noopener noreferrer"
                class="underline underline-offset-4 text-foreground font-medium ml-1"
              >
                Sign up
              </a>
            </div>
          </div>
        </form>
      </CardContent>
    </Card>
  </div>
</template>
