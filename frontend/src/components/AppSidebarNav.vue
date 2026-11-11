<script setup>
import {
  ArchiveIcon as Archive,
  LayoutDashboardIcon as LayoutDashboard,
  PanelsTopLeftIcon as PanelsTopLeft,
  SettingsIcon as Settings,
} from "@lucide/vue";
import {
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";

const props = defineProps({ page: { type: String, required: true } });
const emit = defineEmits(["navigate"]);
const { setOpenMobile } = useSidebar();

function navigate(page) {
  emit("navigate", page);
  setOpenMobile(false);
}
</script>

<template>
  <SidebarContent>
    <SidebarGroup>
      <SidebarGroupLabel>运行</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'dashboard'"
              @click="navigate('dashboard')"
            >
              <LayoutDashboard /><span>概览</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'archive'"
              @click="navigate('archive')"
            >
              <Archive /><span>归档</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
    <SidebarGroup>
      <SidebarGroupLabel>配置</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'config'"
              @click="navigate('config')"
            >
              <Settings /><span>设置</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'home'"
              @click="navigate('home')"
            >
              <PanelsTopLeft /><span>首页编排</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  </SidebarContent>
</template>
