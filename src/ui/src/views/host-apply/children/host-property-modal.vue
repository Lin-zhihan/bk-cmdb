<!--
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2022 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
-->

<template>
  <bk-dialog
    v-model="show"
    :draggable="false"
    :mask-close="true"
    :width="730"
    header-position="left"
    :title="$t('选择字段')"
    @value-change="handleVisibleChange"
    @confirm="handleConfirm"
    @cancel="handleCancel"
  >
    <bk-input v-if="propertyList.length"
      class="search"
      type="text"
      :placeholder="$t('请输入字段名称搜索')"
      clearable
      right-icon="bk-icon icon-search"
      v-model.trim="searchName"
      @input="hanldeFilterProperty">
    </bk-input>
    <bk-checkbox-group v-model="localChecked">
      <ul class="property-list">
        <li class="property-item" v-for="property in propertyList" :key="property.bk_property_id"
          v-show="property.__extra__.visible">
          <bk-checkbox
            :disabled="!property.host_apply_enabled"
            :value="property.id">
            <div
              v-if="!property.host_apply_enabled"
              v-bk-tooltips.top-start="$t('该字段不支持配置')"
              style="outline:none">
              {{property.bk_property_name}}
            </div>
            <div v-else>
              {{property.bk_property_name}}
            </div>
          </bk-checkbox>
        </li>
      </ul>
    </bk-checkbox-group>
  </bk-dialog>
</template>

<script>
  import { mapGetters } from 'vuex'
  export default {
    props: {
      visible: {
        type: Boolean,
        default: false
      },
      checkedList: {
        type: Array,
        default: () => ([])
      }
    },
    data() {
      return {
        show: this.visible,
        localChecked: [],
        searchName: '',
        propertyList: []
      }
    },
    computed: {
      ...mapGetters('hostApply', ['configPropertyList']),
      ...mapGetters('objectBiz', ['bizId'])
    },
    watch: {
      visible(val) {
        this.show = val
      },
      checkedList: {
        handler() {
          this.localChecked = this.checkedList
        },
        immediate: true
      }
    },
    async created() {
      await this.getHostPropertyList()
      this.propertyList = this.$tools.clone(this.configPropertyList)
    },
    methods: {
      async getHostPropertyList() {
        try {
          const data = await this.$store.dispatch('hostApply/getProperties', {
            params: { bk_biz_id: this.bizId },
            config: {
              requestId: 'getHostPropertyList',
              fromCache: true
            }
          })
          this.$store.commit('hostApply/setPropertyList', data)
        } catch (e) {
          console.error(e)
        }
      },
      handleVisibleChange(val) {
        this.$emit('update:visible', val)
      },
      handleConfirm() {
        this.$emit('update:checkedList', this.localChecked)
      },
      handleCancel() {
        this.localChecked = this.checkedList
      },
      hanldeFilterProperty() {
        // 使用visible方式是为了兼容checkbox-group组件
        this.propertyList.forEach((property) => {
          // eslint-disable-next-line no-underscore-dangle
          property.__extra__.visible = property.bk_property_name.indexOf(this.searchName) > -1
        })
        this.propertyList = [...this.propertyList]
      }
    }
  }
</script>

<style lang="scss" scoped>
    .search {
        width: 280px;
        margin-bottom: 10px;
    }
    .property-list {
        display: flex;
        flex-wrap: wrap;
        align-content: flex-start;
        height: 264px;
        @include scrollbar-y;

        .property-item {
            flex: 0 0 33.3333%;
            margin: 8px 0;
        }
    }
</style>
