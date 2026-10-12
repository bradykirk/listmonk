<template>
  <section class="brands">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          Brands
          <span v-if="brands.length">({{ brands.length }})</span>
        </h1>
      </div>
      <div class="column has-text-right">
        <b-field v-if="$can('settings:manage')" expanded>
          <b-button expanded type="is-primary" icon-left="plus" class="btn-new" @click="showNewForm"
            data-cy="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
      </div>
    </header>

    <b-table :data="brands" :loading="isLoading" hoverable default-sort="sort_order">
      <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')" sortable>
        <a href="#" @click.prevent="showEditForm(props.row)" data-cy="brand-name">{{ props.row.name }}</a>
        <p class="is-size-7 has-text-grey">{{ props.row.slug }}</p>
      </b-table-column>

      <b-table-column v-slot="props" field="list_tags" label="List tags">
        <b-taglist>
          <b-tag v-for="t in props.row.list_tags" :key="t" class="is-small">{{ t }}</b-tag>
        </b-taglist>
      </b-table-column>

      <b-table-column v-slot="props" field="messengers" label="Messengers">
        <b-taglist>
          <b-tag v-for="m in props.row.messengers" :key="m" class="is-small">{{ m }}</b-tag>
        </b-taglist>
      </b-table-column>

      <b-table-column v-slot="props" field="from_domains" label="From domains">
        <b-taglist>
          <b-tag v-for="d in props.row.from_domains" :key="d" class="is-small">{{ d }}</b-tag>
        </b-taglist>
      </b-table-column>

      <b-table-column v-slot="props" field="list_count" label="Lists" numeric sortable>
        {{ props.row.list_count }}
      </b-table-column>

      <b-table-column v-slot="props" field="sort_order" label="Sort order" numeric sortable>
        {{ props.row.sort_order }}
      </b-table-column>

      <b-table-column v-slot="props" cell-class="actions" align="right">
        <div v-if="$can('settings:manage')">
          <a href="#" @click.prevent="showEditForm(props.row)" data-cy="btn-edit"
            :aria-label="$t('globals.buttons.edit')">
            <b-icon icon="pencil-outline" size="is-small" />
          </a>
          <a href="#" @click.prevent="$utils.confirm(null, () => deleteBrand(props.row))" data-cy="btn-delete"
            :aria-label="$t('globals.buttons.delete')">
            <b-icon icon="trash-can-outline" size="is-small" />
          </a>
        </div>
      </b-table-column>

      <template #empty v-if="!isLoading">
        <empty-placeholder />
      </template>
    </b-table>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isFormVisible" :width="600">
      <form @submit.prevent="onSubmit">
        <div class="modal-card content" style="width: auto">
          <header class="modal-card-head">
            <h4>{{ isEditing ? form.name : 'New brand' }}</h4>
          </header>
          <section expanded class="modal-card-body">
            <b-field :label="$t('globals.fields.name')" label-position="on-border">
              <b-input :maxlength="200" v-model="form.name" name="name" required data-cy="brand-form-name" />
            </b-field>

            <b-field v-if="!isEditing" label="Slug" label-position="on-border"
              message="Lowercase letters, digits, and hyphens. Starts with a letter. Cannot change later.">
              <b-input v-model="form.slug" name="slug" pattern="^[a-z][a-z0-9\-]{1,30}$" required
                data-cy="brand-form-slug" />
            </b-field>

            <b-field label="List tags" label-position="on-border"
              message="A list belongs to the brand when it has one of these tags. The first tag is added to new lists.">
              <b-taginput v-model="form.list_tags" name="list_tags" ellipsis icon="tag-outline" placeholder="List tags"
                data-cy="brand-form-tags" />
            </b-field>

            <b-field label="Messengers" message="Leave all unchecked to allow every messenger.">
              <div class="block">
                <b-checkbox v-for="m in messengerChoices" :key="m" v-model="form.messengers" :native-value="m">
                  {{ m }}
                </b-checkbox>
              </div>
            </b-field>

            <b-field label="From domains" label-position="on-border">
              <b-taginput v-model="form.from_domains" name="from_domains" ellipsis icon="tag-outline"
                placeholder="example.com" data-cy="brand-form-domains" />
            </b-field>

            <b-field label="Sort order" label-position="on-border">
              <b-numberinput v-model="form.sort_order" name="sort_order" controls-position="compact"
                data-cy="brand-form-sort" />
            </b-field>
          </section>
          <footer class="modal-card-foot has-text-right">
            <b-button @click="isFormVisible = false">
              {{ $t('globals.buttons.close') }}
            </b-button>
            <b-button v-if="$can('settings:manage')" native-type="submit" type="is-primary" :loading="isSaving"
              data-cy="btn-save">
              {{ $t('globals.buttons.save') }}
            </b-button>
          </footer>
        </div>
      </form>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import EmptyPlaceholder from '../../components/EmptyPlaceholder.vue';
import { getTabBrand, selectBrand } from '../../brand';

const blank = () => ({
  slug: '',
  name: '',
  list_tags: [],
  messengers: [],
  from_domains: [],
  sort_order: 0,
});

export default Vue.extend({
  name: 'Brands',

  components: { EmptyPlaceholder },

  data() {
    return {
      isLoading: false,
      isSaving: false,
      isFormVisible: false,
      isEditing: false,
      form: blank(),
    };
  },

  methods: {
    // Load the brands (with list_count) into the store, which the navbar switcher also uses.
    getBrands() {
      this.isLoading = true;
      return this.$api.getBrands().then((data) => {
        this.$store.commit('setBrands', data);
      }).finally(() => { this.isLoading = false; });
    },

    showNewForm() {
      this.isEditing = false;
      this.form = blank();
      this.isFormVisible = true;
    },

    showEditForm(b) {
      this.isEditing = true;
      // Copy every field: the server replaces the whole brand on save.
      this.form = {
        slug: b.slug,
        name: b.name,
        list_tags: [...(b.list_tags || [])],
        messengers: [...(b.messengers || [])],
        from_domains: [...(b.from_domains || [])],
        sort_order: b.sort_order || 0,
      };
      this.isFormVisible = true;
    },

    onSubmit() {
      this.isSaving = true;
      const call = this.isEditing ? this.$api.updateBrand : this.$api.createBrand;
      call({ ...this.form }).then(() => {
        this.isFormVisible = false;
        this.$utils.toast(this.$t('globals.messages.updated', { name: this.form.name }));
        return this.getBrands();
      }).finally(() => { this.isSaving = false; });
    },

    deleteBrand(b) {
      // The server refuses with a 409 and a message when the brand is in use;
      // the API layer shows that message as a toast.
      this.$api.deleteBrand(b.slug).then(() => {
        this.$utils.toast(this.$t('globals.messages.deleted', { name: b.name }));
        if (getTabBrand() === b.slug) {
          selectBrand('');
          return null;
        }
        return this.getBrands();
      }).catch(() => {});
    },
  },

  computed: {
    ...mapState(['brands', 'serverConfig']),

    messengerChoices() {
      return this.serverConfig.messengers || [];
    },
  },

  created() {
    this.$root.$on('page.refresh', this.getBrands);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.getBrands);
  },

  mounted() {
    this.getBrands();
  },
});
</script>
