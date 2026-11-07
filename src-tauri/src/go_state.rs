//! Synchronous typed row transport for Go-owned SQLite. This module opens no database.
use rusqlite::{
    types::{FromSql, ToSql, ToSqlOutput, Value as SqlValue, ValueRef},
    Error, Result,
};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};

pub trait Parameters {
    fn values(self) -> Result<Vec<Value>>;
}
fn value(v: &dyn ToSql) -> Result<Value> {
    let sql = v.to_sql()?;
    let borrowed = match &sql {
        ToSqlOutput::Borrowed(v) => *v,
        ToSqlOutput::Owned(v) => ValueRef::from(v),
        _ => return Err(Error::InvalidQuery),
    };
    match borrowed {
        ValueRef::Null => Ok(Value::Null),
        ValueRef::Integer(n) => Ok(json!(n)),
        ValueRef::Real(n) => Ok(json!(n)),
        ValueRef::Text(v) => Ok(json!(
            std::str::from_utf8(v).map_err(|_| Error::InvalidQuery)?
        )),
        _ => Err(Error::InvalidQuery),
    }
}
impl Parameters for [(); 0] {
    fn values(self) -> Result<Vec<Value>> {
        Ok(Vec::new())
    }
}
impl<T: ToSql> Parameters for &[T] {
    fn values(self) -> Result<Vec<Value>> {
        self.iter().map(|v| value(v)).collect()
    }
}
impl<T: ToSql, const N: usize> Parameters for &[T; N] {
    fn values(self) -> Result<Vec<Value>> {
        self.as_slice().values()
    }
}
macro_rules! arrays{($($n:expr),*)=>{$(impl<T:ToSql> Parameters for [T;$n]{fn values(self)->Result<Vec<Value>>{self.as_slice().values()}})*}}
arrays!(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16);
#[derive(serde::Deserialize)]
struct Output {
    rows: Vec<Vec<Value>>,
    changed: usize,
}
pub struct Row {
    values: Vec<SqlValue>,
}
impl Row {
    fn from(values: Vec<Value>) -> Result<Self> {
        Ok(Self {
            values: values
                .into_iter()
                .map(|v| match v {
                    Value::Null => Ok(SqlValue::Null),
                    Value::Bool(b) => Ok(SqlValue::Integer(i64::from(b))),
                    Value::Number(n) => n
                        .as_i64()
                        .map(SqlValue::Integer)
                        .or_else(|| n.as_f64().map(SqlValue::Real))
                        .ok_or(Error::InvalidQuery),
                    Value::String(s) => Ok(SqlValue::Text(s)),
                    _ => Err(Error::InvalidQuery),
                })
                .collect::<Result<_>>()?,
        })
    }
    pub fn get<I: TryInto<usize>, T: FromSql>(&self, index: I) -> Result<T> {
        let index = index.try_into().map_err(|_| Error::InvalidQuery)?;
        let v = self
            .values
            .get(index)
            .ok_or(Error::InvalidColumnIndex(index))?;
        T::column_result(ValueRef::from(v)).map_err(|_| Error::InvalidQuery)
    }
}
pub struct Connection {
    path: PathBuf,
}
impl Connection {
    pub fn open(path: impl AsRef<Path>) -> Result<Self> {
        Ok(Self {
            path: path.as_ref().to_path_buf(),
        })
    }
    fn call(
        &self,
        method: &str,
        query: &str,
        params: Vec<Value>,
        batch: Option<Vec<Value>>,
    ) -> Result<Output> {
        let value = crate::metadata_transport::call(
            method,
            json!({"path":self.path,"query":query,"params":params,"batch":batch}),
        )
        .map_err(|e| Error::ToSqlConversionFailure(Box::new(e)))?;
        serde_json::from_value(value).map_err(|_| Error::InvalidQuery)
    }
    pub fn execute(&self, query: &str, params: impl Parameters) -> Result<usize> {
        Ok(self
            .call("state.execute", query, params.values()?, None)?
            .changed)
    }
    pub fn execute_batch(&self, query: &str) -> Result<()> {
        self.call("state.initialize", query, vec![], None)?;
        Ok(())
    }
    pub fn prepare<'a>(&'a self, query: &str) -> Result<Statement<'a>> {
        Ok(Statement {
            connection: self,
            query: query.into(),
        })
    }
    pub fn query_row<T>(
        &self,
        query: &str,
        params: impl Parameters,
        read: impl FnOnce(&Row) -> Result<T>,
    ) -> Result<T> {
        let output = self.call("state.query", query, params.values()?, None)?;
        let row = output
            .rows
            .into_iter()
            .next()
            .ok_or(Error::QueryReturnedNoRows)?;
        read(&Row::from(row)?)
    }
}
pub struct Statement<'a> {
    connection: &'a Connection,
    query: String,
}
impl Statement<'_> {
    pub fn query_map<T>(
        &mut self,
        params: impl Parameters,
        mut read: impl FnMut(&Row) -> Result<T>,
    ) -> Result<std::vec::IntoIter<Result<T>>> {
        let output = self
            .connection
            .call("state.query", &self.query, params.values()?, None)?;
        Ok(output
            .rows
            .into_iter()
            .map(|v| Row::from(v).and_then(|r| read(&r)))
            .collect::<Vec<_>>()
            .into_iter())
    }
}
